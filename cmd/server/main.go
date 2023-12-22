package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"runtime"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/indexer"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
)

const (
	accessURL = "access-001.devnet49.nodes.onflow.org:9000"
	coinbase  = "0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"
)

func main() {
	var network, coinbase string

	flag.StringVar(&network, "network", "testnet", "network to connect the gateway to")
	flag.StringVar(&coinbase, "coinbase", coinbase, "coinbase address to use for fee collection")
	flag.Parse()

	config := &api.Config{}
	config.Coinbase = common.HexToAddress(coinbase)
	if network == "testnet" {
		config.ChainID = emulator.FlowEVMTestnetChainID
	} else if network == "mainnet" {
		config.ChainID = emulator.FlowEVMMainnetChainID
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
	flowClient, err := grpc.NewClient(accessURL)
	if err != nil {
		panic(err)
	}

	latestBlockHeader, err := flowClient.GetLatestBlockHeader(ctx, true)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Latest Block Height: %d\n", latestBlockHeader.Height)
	fmt.Printf("Latest Block ID: %s\n", latestBlockHeader.ID)

	chain, err := indexer.GetChain(ctx, accessURL)
	if err != nil {
		log.Fatalf("could not get chain: %v", err)
	}

	execClient, err := indexer.NewExecutionDataClient(accessURL, chain)
	if err != nil {
		log.Fatalf("could not create execution data client: %v", err)
	}

	sub, err := execClient.SubscribeEvents(
		ctx,
		flow.ZeroID,
		latestBlockHeader.Height,
		indexer.EventFilter{
			Contracts: []string{"A.7e60df042a9c0868.FlowToken"},
		},
		1,
	)
	if err != nil {
		log.Fatalf("could not subscribe to execution data: %v", err)
	}

	reconnect := func(height uint64) {
		fmt.Printf("Reconnecting at block %d\n", height)

		var err error
		execClient, err := indexer.NewExecutionDataClient(accessURL, chain)
		if err != nil {
			log.Fatalf("could not create execution data client: %v", err)
		}

		sub, err = execClient.SubscribeEvents(
			ctx,
			flow.ZeroID,
			height,
			indexer.EventFilter{
				Contracts: []string{"A.7e60df042a9c0868.FlowToken"},
			},
			1,
		)
		if err != nil {
			log.Fatalf("could not subscribe to execution data: %v", err)
		}
	}

	// track the most recently seen block height. we will use this when reconnecting
	// the first response should be for latestBlockHeader.Height
	lastHeight := latestBlockHeader.Height - 1
	for {
		select {
		case <-ctx.Done():
			return
		case response, ok := <-sub.Channel():
			if response.Height != lastHeight+1 {
				log.Fatalf("missed events response for block %d", lastHeight+1)
				reconnect(lastHeight)
				continue
			}

			if !ok {
				if sub.Err() != nil {
					log.Fatalf("error in subscription: %v", sub.Err())
					return // graceful shutdown
				}
				log.Fatalf("subscription closed - reconnecting")
				reconnect(lastHeight + 1)
				continue
			}

			log.Printf("block %d %s:", response.Height, response.BlockID)
			if len(response.Events) > 0 {
				store.StoreBlockHeight(ctx, response.Height)
			}
			for _, event := range response.Events {
				log.Printf("  %s", event.Type)
			}

			lastHeight = response.Height
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
