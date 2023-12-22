package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"runtime"

	goGrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/rs/zerolog"
)

const (
	accessURL    = "access-001.devnet49.nodes.onflow.org:9000"
	coinbaseAddr = "0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"
)

func main() {
	var network, coinbase string

	flag.StringVar(&network, "network", "testnet", "network to connect the gateway to")
	flag.StringVar(&coinbase, "coinbase", coinbaseAddr, "coinbase address to use for fee collection")
	flag.Parse()

	config := &api.Config{}
	config.Coinbase = common.HexToAddress(coinbase)
	if network == "testnet" {
		config.ChainID = api.FlowEVMTestnetChainID
	} else if network == "mainnet" {
		config.ChainID = api.FlowEVMMainnetChainID
	} else {
		panic(fmt.Errorf("unknown network: %s", network))
	}

	store := storage.NewStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runServer(config, store)
	runIndexer(ctx, store)

	runtime.Goexit()
}

func runIndexer(ctx context.Context, store *storage.Store) {
	flowClient, err := grpc.NewBaseClient(
		accessURL,
		goGrpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(err)
	}

	latestBlockHeader, err := flowClient.GetLatestBlockHeader(ctx, true)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Latest Block Height: %d\n", latestBlockHeader.Height)
	fmt.Printf("Latest Block ID: %s\n", latestBlockHeader.ID)

	data, errChan, initErr := flowClient.SubscribeEventsByBlockHeight(
		ctx,
		latestBlockHeader.Height,
		flow.EventFilter{
			Contracts: []string{"A.7e60df042a9c0868.FlowToken"},
		},
		grpc.WithHeartbeatInterval(1),
	)
	if initErr != nil {
		log.Fatalf("could not subscribe to events: %v", initErr)
	}

	reconnect := func(height uint64) {
		fmt.Printf("Reconnecting at block %d\n", height)

		var err error
		flowClient, err := grpc.NewBaseClient(
			accessURL,
			goGrpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			log.Fatalf("could not create flow client: %v", err)
		}

		data, errChan, initErr = flowClient.SubscribeEventsByBlockHeight(
			ctx,
			latestBlockHeader.Height,
			flow.EventFilter{
				Contracts: []string{"A.7e60df042a9c0868.FlowToken"},
			},
			grpc.WithHeartbeatInterval(1),
		)
		if initErr != nil {
			log.Fatalf("could not subscribe to events: %v", initErr)
		}
	}

	// track the most recently seen block height. we will use this when reconnecting
	// the first response should be for latestBlockHeader.Height
	lastHeight := latestBlockHeader.Height - 1
	for {
		select {
		case <-ctx.Done():
			return

		case response, ok := <-data:
			if !ok {
				if ctx.Err() != nil {
					return // graceful shutdown
				}
				fmt.Println("subscription closed - reconnecting")
				reconnect(lastHeight + 1)
				continue
			}

			if response.Height != lastHeight+1 {
				fmt.Printf("missed events response for block %d\n", lastHeight+1)
				reconnect(lastHeight)
				continue
			}

			fmt.Printf("block %d %s:\n", response.Height, response.BlockID)
			if len(response.Events) > 0 {
				store.StoreBlockHeight(ctx, response.Height)
			}
			for _, event := range response.Events {
				fmt.Printf("  %s\n", event.Type)
			}

			lastHeight = response.Height

		case err, ok := <-errChan:
			if !ok {
				if ctx.Err() != nil {
					return // graceful shutdown
				}
				// unexpected close
				reconnect(lastHeight + 1)
				continue
			}

			fmt.Printf("~~~ ERROR: %s ~~~\n", err.Error())
			reconnect(lastHeight + 1)
			continue
		}
	}
}

func runServer(config *api.Config, store *storage.Store) {
	srv := api.NewHTTPServer(zerolog.Logger{}, rpc.DefaultHTTPTimeouts)
	supportedAPIs := api.SupportedAPIs(config, store)
	srv.EnableRPC(supportedAPIs)
	srv.EnableWS(supportedAPIs)
	srv.SetListenAddr("localhost", 8545)
	err := srv.Start()
	if err != nil {
		panic(err)
	}
	fmt.Println("Server Started: ", srv.ListenAddr())
}
