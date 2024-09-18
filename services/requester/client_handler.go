package requester

import (
	"context"
	"math/big"
	"reflect"
	"sync"
	"time"

	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/go-ethereum/common"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/services/state"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

var _ EVMClient = &ClientHandler{}

// ClientHandler handles remote and local client for executing EVM operations.
// The handler contains logic that can switch between using local or remote client
// and implements error handling logic that can prefer either remote result or
// local result.
type ClientHandler struct {
	remote    *RemoteClient
	config    *config.Config
	store     *pebble.Storage
	blocks    storage.BlockIndexer
	receipts  storage.ReceiptIndexer
	logger    zerolog.Logger
	collector metrics.Collector
}

func NewClientHandler(
	config *config.Config,
	store *pebble.Storage,
	txPool *TxPool,
	signer crypto.Signer,
	client *CrossSporkClient,
	blocks storage.BlockIndexer,
	receipts storage.ReceiptIndexer,
	logger zerolog.Logger,
	collector metrics.Collector,
) (*ClientHandler, error) {
	remote, err := NewRemote(client, config, signer, logger, blocks, txPool, collector)
	if err != nil {
		return nil, err
	}

	return &ClientHandler{
		remote:    remote,
		config:    config,
		store:     store,
		blocks:    blocks,
		receipts:  receipts,
		logger:    logger,
		collector: collector,
	}, nil
}

func (c *ClientHandler) SendRawTransaction(ctx context.Context, data []byte) (common.Hash, error) {
	// always use remote client
	return c.remote.SendRawTransaction(ctx, data)
}

func (c *ClientHandler) GetBalance(
	ctx context.Context,
	address common.Address,
	height uint64,
) (*big.Int, error) {
	local, err := c.localClient(height)
	if err != nil {
		return nil, err
	}

	return handleCall(func() (*big.Int, error) {
		return local.GetBalance(ctx, address, height)
	}, func() (*big.Int, error) {
		return c.remote.GetBalance(ctx, address, height)
	}, c.logger.With().Str("client-call", "get balance").Logger())
}

func (c *ClientHandler) Call(
	ctx context.Context,
	data []byte,
	from common.Address,
	height uint64,
) ([]byte, error) {
	local, err := c.localClient(height)
	if err != nil {
		return nil, err
	}

	return handleCall(func() ([]byte, error) {
		return local.Call(ctx, data, from, height)
	}, func() ([]byte, error) {
		return c.remote.Call(ctx, data, from, height)
	}, c.logger.With().Str("client-call", "call").Logger())
}

func (c *ClientHandler) EstimateGas(
	ctx context.Context,
	data []byte,
	from common.Address,
	height uint64,
) (uint64, error) {
	local, err := c.localClient(height)
	if err != nil {
		return 0, err
	}

	return handleCall(func() (uint64, error) {
		return local.EstimateGas(ctx, data, from, height)
	}, func() (uint64, error) {
		return c.remote.EstimateGas(ctx, data, from, height)
	}, c.logger.With().Str("client-call", "estimate gas").Logger())
}

func (c *ClientHandler) GetNonce(
	ctx context.Context,
	address common.Address,
	height uint64,
) (uint64, error) {
	local, err := c.localClient(height)
	if err != nil {
		return 0, err
	}

	return handleCall(func() (uint64, error) {
		return local.GetNonce(ctx, address, height)
	}, func() (uint64, error) {
		return c.remote.GetNonce(ctx, address, height)
	}, c.logger.With().Str("client-call", "get nonce").Logger())
}

func (c *ClientHandler) GetCode(
	ctx context.Context,
	address common.Address,
	height uint64,
) ([]byte, error) {
	local, err := c.localClient(height)
	if err != nil {
		return nil, err
	}

	return handleCall(func() ([]byte, error) {
		return local.GetCode(ctx, address, height)
	}, func() ([]byte, error) {
		return c.remote.GetCode(ctx, address, height)
	}, c.logger.With().Str("client-call", "get code").Logger())
}

func (c *ClientHandler) GetLatestEVMHeight(ctx context.Context) (uint64, error) {
	// we use the remote client to get the latest height from the network
	// be careful, because this height might not yet be indexed or executed locally
	// so don't use this height to then query the state, always use the latest
	// executed height to query the state.
	return c.remote.GetLatestEVMHeight(ctx)
}

func (c *ClientHandler) GetStorageAt(
	ctx context.Context,
	address common.Address,
	hash common.Hash,
	height uint64,
) (common.Hash, error) {
	local, err := c.localClient(height)
	if err != nil {
		return common.Hash{}, err
	}

	return handleCall(func() (common.Hash, error) {
		return local.GetStorageAt(ctx, address, hash, height)
	}, func() (common.Hash, error) {
		return c.remote.GetStorageAt(ctx, address, hash, height)
	}, c.logger.With().Str("client-call", "get storage at").Logger())
}

func (c *ClientHandler) localClient(height uint64) (*LocalClient, error) {
	block, err := c.blocks.GetByHeight(height)
	if err != nil {
		return nil, err
	}

	blockState, err := state.NewBlockState(
		block,
		pebble.NewRegister(c.store, height, nil),
		c.config.FlowNetworkID,
		c.blocks,
		c.receipts,
		c.logger,
		nil,
	)
	if err != nil {
		return nil, err
	}

	return NewLocalClient(blockState, c.blocks), nil
}

// handleCall takes in local and remote call and implements error handling logic to return
// correct result, it also compares the results in case there are no errors and reports any differences.
func handleCall[T any](
	local func() (T, error),
	remote func() (T, error),
	logger zerolog.Logger,
) (T, error) {
	logger.Info().Msg("executing state client call")

	var localErr, remoteErr error
	var localRes, remoteRes T

	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		s := time.Now()
		localRes, localErr = local()
		logger.Info().
			Int64("execution-ns", time.Since(s).Nanoseconds()).
			Msg("local call executed")
		wg.Done()
	}()

	go func() {
		s := time.Now()
		remoteRes, remoteErr = remote()
		logger.Info().
			Int64("execution-ns", time.Since(s).Nanoseconds()).
			Msg("remote call executed")
		wg.Done()
	}()

	wg.Wait()

	// happy case, both errs are nil and results are same
	if localErr == nil && remoteErr == nil {
		// if results are not same log the diff
		if !reflect.DeepEqual(localRes, remoteRes) {
			logger.Error().
				Any("local", localRes).
				Any("remote", remoteRes).
				Msg("results from local and remote client are note the same")
		}
	}

	// make sure if both return an error the errors are the same
	if localErr != nil && remoteErr != nil &&
		localErr.Error() != remoteErr.Error() {
		logger.Error().
			Str("local", localErr.Error()).
			Str("remote", remoteErr.Error()).
			Msg("errors from local and remote client are note the same")
	}

	// if remote received an error but local call worked, return the local result
	// this can be due to rate-limits or pruned state on AN/EN
	if localErr == nil && remoteErr != nil {
		logger.Warn().
			Str("remote-error", remoteErr.Error()).
			Any("local-result", localRes).
			Msg("error from remote client but not from local client")

		// todo check the error type equals to a whitelist (rate-limits, pruned state (EN)...)

		return localRes, nil
	}

	// if remote succeeded but local received an error this is a bug or in case of a
	// call or gas estimation that uses cadence arch a failure is expected, because
	// the local state doesn't have a way to return values for cadence arch calls because
	// no transaction produced a precompiled calls input/output mock for it.
	// todo find a way to possibly detect such calls and ignore errors.
	if localErr != nil && remoteErr == nil {
		logger.Error().
			Str("local-error", localErr.Error()).
			Any("remote-result", remoteRes).
			Msg("error from local client but not from remote client")
	}

	return remoteRes, remoteErr
}
