package main

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"runtime"
	"time"

	goGrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
)

const (
	defaultAccessURL = grpc.EmulatorHost
	coinbaseAddr     = "0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"
)

var blockExecutedType = (types.EVMLocation{}).TypeID(nil, string(types.EventTypeBlockExecuted))
var txExecutedType = (types.EVMLocation{}).TypeID(nil, string(types.EventTypeTransactionExecuted))

var evmEventTypes = []string{
	string(blockExecutedType),
	string(txExecutedType),
}

func main() {
	var network, coinbase string
	var gasPrice int64

	flag.StringVar(&network, "network", "testnet", "network to connect the gateway to")
	flag.StringVar(&coinbase, "coinbase", coinbaseAddr, "coinbase address to use for fee collection")
	flag.Int64Var(&gasPrice, "gasPrice", api.DefaultGasPrice.Int64(), "gas price for transactions")
	flag.Parse()

	config := &api.Config{
		Coinbase: common.HexToAddress(coinbase),
		GasPrice: big.NewInt(gasPrice),
	}
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

	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()
	runServer(config, store, logger)
	runIndexer(ctx, store, logger)

	runtime.Goexit()
}

func runIndexer(ctx context.Context, store *storage.Store, logger zerolog.Logger) {
	flowClient, err := grpc.NewBaseClient(
		defaultAccessURL,
		goGrpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(err)
	}

	// TODO(m-Peter) The starting height from which the indexer should
	// begins, should either be retrieved from storage (latest height + 1),
	// or should be specified through a command-line flag (when starting
	// from scratch).
	latestBlockHeader, err := flowClient.GetLatestBlockHeader(ctx, true)
	if err != nil {
		panic(err)
	}
	logger.Info().Msgf("Latest Block Height: %d", latestBlockHeader.Height)

	connect := func(height uint64, waitTime time.Duration) (<-chan flow.BlockEvents, <-chan error, error) {
		logger.Info().Msgf("Connecting at block height: %d", height)
		// TODO(m-Peter) We should add a proper library for retrying
		// with configurable backoffs, such as https://github.com/sethvargo/go-retry.
		time.Sleep(waitTime)

		var err error
		flowClient, err := grpc.NewBaseClient(
			defaultAccessURL,
			goGrpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			logger.Error().Msgf("could not create flow client: %v", err)
		}

		return flowClient.SubscribeEventsByBlockHeight(
			ctx,
			latestBlockHeader.Height,
			flow.EventFilter{
				EventTypes: evmEventTypes,
			},
			grpc.WithHeartbeatInterval(1),
		)
	}

	data, errChan, initErr := connect(latestBlockHeader.Height, 0)
	if initErr != nil {
		logger.Error().Msgf("could not subscribe to events: %v", initErr)
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
				logger.Error().Msg("subscription closed - reconnecting")
				connect(lastHeight+1, 10)
				continue
			}

			if response.Height != lastHeight+1 {
				logger.Error().Msgf("missed events response for block %d", lastHeight+1)
				connect(lastHeight, 10)
				continue
			}

			logger.Info().Msgf("block %d %s:", response.Height, response.BlockID)

			for _, event := range response.Events {
				logger.Info().Msgf("  %s", event.Value)
				if event.Type == "evm.TransactionExecuted" {
					err := store.StoreTransaction(ctx, event.Value)
					if err != nil {
						logger.Error().Msgf("got error when storing tx: %s", err)
					}
					store.UpdateAccountNonce(ctx, event.Value)
					store.StoreLog(ctx, event.Value)
				}
				if event.Type == "evm.BlockExecuted" {
					err := store.StoreBlock(ctx, event.Value)
					if err != nil {
						logger.Error().Msgf("got error when storing block: %s", err)
					}
				}
			}

			lastHeight = response.Height

		case err, ok := <-errChan:
			if !ok {
				if ctx.Err() != nil {
					return // graceful shutdown
				}
				// unexpected close
				connect(lastHeight+1, 10)
				continue
			}

			logger.Error().Msgf("ERROR: %v", err)
			connect(lastHeight+1, 10)
			continue
		}
	}
}

func runServer(config *api.Config, store *storage.Store, logger zerolog.Logger) {
	srv := api.NewHTTPServer(logger, rpc.DefaultHTTPTimeouts)
	flowClient, err := api.NewFlowClient(grpc.EmulatorHost)
	if err != nil {
		panic(err)
	}
	blockchainAPI := api.NewBlockChainAPI(config, store, flowClient)
	supportedAPIs := api.SupportedAPIs(blockchainAPI)

	srv.EnableRPC(supportedAPIs)
	srv.EnableWS(supportedAPIs)

	srv.SetListenAddr("", 8545)

	err = srv.Start()
	if err != nil {
		panic(err)
	}
	logger.Info().Msgf("Server Started: %s", srv.ListenAddr())
}
