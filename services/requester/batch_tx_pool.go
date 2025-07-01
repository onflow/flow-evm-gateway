package requester

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	flowGo "github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-evm-gateway/services/requester/keystore"
)

const (
	eoaActivityCacheSize = 10_000
	// Buffer capacity of the channel used for individual transaction
	// submission. Since `BatchTxPool.Add()` is called quite frequently,
	// we don't want sending to the channel to block, until there's a
	// receive ready. That's why we set a high buffer capacity, to allow
	// `BatchTxPool.Add()` to do its job quickly and release any resources
	// held, like locks etc.
	txChanBufferSize = 1_000
)

type pooledEvmTx struct {
	txPayload cadence.String
	nonce     uint64
}

// BatchTxPool is a `TxPool` implementation that collects and groups
// transactions based on their EOA signer, and submits them for execution
// using a batch.
//
// The underlying Cadence EVM API used, is `EVM.batchRun`, instead of the
// `EVM.run` used in `SingleTxPool`.
//
// The main advantage of this implementation over the `SingleTxPool`, is the
// guarantee that transactions originated from the same EOA address, which
// arrive in a short time interval (about the same as Flow's block production rate),
// will be executed in the same order their arrived.
// This helps to reduce the nonce mismatch errors which mainly occur from the
// re-ordering of Cadence transactions that happens from Collection nodes.
type BatchTxPool struct {
	*SingleTxPool
	pooledTxs   map[gethCommon.Address][]pooledEvmTx
	txChan      chan cadence.String
	txMux       sync.Mutex
	eoaActivity *expirable.LRU[gethCommon.Address, time.Time]
}

var _ TxPool = &BatchTxPool{}

func NewBatchTxPool(
	ctx context.Context,
	client *CrossSporkClient,
	transactionsPublisher *models.Publisher[*gethTypes.Transaction],
	logger zerolog.Logger,
	config config.Config,
	collector metrics.Collector,
	keystore *keystore.KeyStore,
) *BatchTxPool {
	// initialize the available keys metric since it is only updated when sending a tx
	collector.AvailableSigningKeys(keystore.AvailableKeys())

	singleTxPool := NewSingleTxPool(
		client,
		transactionsPublisher,
		logger,
		config,
		collector,
		keystore,
	)

	eoaActivity := expirable.NewLRU[gethCommon.Address, time.Time](
		eoaActivityCacheSize,
		nil,
		config.TxBatchInterval,
	)
	batchPool := &BatchTxPool{
		SingleTxPool: singleTxPool,
		pooledTxs:    make(map[gethCommon.Address][]pooledEvmTx),
		txChan:       make(chan cadence.String, txChanBufferSize),
		txMux:        sync.Mutex{},
		eoaActivity:  eoaActivity,
	}

	go batchPool.processPooledTransactions(ctx)

	return batchPool
}

// Add adds the EVM transaction to the tx pool, grouped with the rest of the
// transactions from the same EOA signer.
// After the configured `TxBatchInterval`, the collected transations
// are batched and sent to the Flow network using `EVM.batchRun`, for execution.
func (t *BatchTxPool) Add(
	ctx context.Context,
	tx *gethTypes.Transaction,
) error {
	t.txPublisher.Publish(tx) // publish pending transaction event

	// tx adding should be blocking, so we don't have races when
	// pooled transactions are being processed in the background.
	t.txMux.Lock()
	defer t.txMux.Unlock()

	from, err := gethTypes.Sender(gethTypes.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		return fmt.Errorf("failed to derive the sender: %w", err)
	}
	txData, err := tx.MarshalBinary()
	if err != nil {
		return err
	}
	hexEncodedTx, err := cadence.NewString(hex.EncodeToString(txData))
	if err != nil {
		return err
	}

	// Scenarios
	// 1. EOA activity not found:
	// => We send the transaction individually, without adding it
	// to the batch pool.
	//
	// 2. EOA activity found AND it was more than [X] seconds ago:
	// => We send the transaction individually, without adding it
	// to the batch pool.
	//
	// 3. EOA activity found AND it was less than [X] seconds ago:
	// => We add the transaction to the batch pool, so that it gets
	// processed and submitted according to the configured
	// `TxBatchInterval`.
	//
	// For all 3 cases, we record the activity time for the next
	// transactions that might come from the same EOA.
	// [X] is equal to the configured `TxBatchInterval` duration.
	lastActivityTime, found := t.eoaActivity.Get(from)
	if !found || time.Since(lastActivityTime) > t.config.TxBatchInterval {
		t.txChan <- hexEncodedTx
	} else {
		userTx := pooledEvmTx{txPayload: hexEncodedTx, nonce: tx.Nonce()}
		t.pooledTxs[from] = append(t.pooledTxs[from], userTx)
	}

	t.eoaActivity.Add(from, time.Now())

	return nil
}

func (t *BatchTxPool) processPooledTransactions(ctx context.Context) {
	ticker := time.NewTicker(t.config.TxBatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			latestBlock, account, err := t.fetchFlowLatestBlockAndCOA(ctx)
			if err != nil {
				t.logger.Error().Err(err).Msg(
					"failed to get COA / latest Flow block on batch tx submission",
				)
				continue
			}

			// Take a copy here to allow `Add()` to continue accept
			// incoming EVM transactions, without blocking until the
			// batch transactions are submitted.
			t.txMux.Lock()
			txsGroupedByAddress := t.pooledTxs
			t.pooledTxs = make(map[gethCommon.Address][]pooledEvmTx)
			t.txMux.Unlock()

			for address, pooledTxs := range txsGroupedByAddress {
				err := t.batchSubmitTransactionsForSameAddress(
					ctx,
					latestBlock,
					account,
					pooledTxs,
				)
				if err != nil {
					t.logger.Error().Err(err).Msgf(
						"failed to submit Flow transaction from BatchTxPool for EOA: %s",
						address.Hex(),
					)
					continue
				}
			}
		case hexEncodedTx := <-t.txChan:
			if err := t.submitSingleTransaction(ctx, hexEncodedTx); err != nil {
				t.logger.Error().Err(err).Msg(
					"failed to submit Flow transaction from BatchTxPool for single EOA tx",
				)
				continue
			}
		}
	}
}

func (t *BatchTxPool) batchSubmitTransactionsForSameAddress(
	ctx context.Context,
	latestBlock *flow.Block,
	account *flow.Account,
	pooledTxs []pooledEvmTx,
) error {
	// Sort the transactions based on their nonce, to make sure
	// that no re-ordering has happened due to races etc.
	sort.Slice(pooledTxs, func(i, j int) bool {
		return pooledTxs[i].nonce < pooledTxs[j].nonce
	})

	hexEncodedTxs := make([]cadence.Value, len(pooledTxs))
	for i, txPayload := range pooledTxs {
		hexEncodedTxs[i] = txPayload.txPayload
	}

	coinbaseAddress, err := cadence.NewString(t.config.Coinbase.Hex())
	if err != nil {
		return err
	}

	script := replaceAddresses(batchRunTxScript, t.config.FlowNetworkID)
	flowTx, err := t.buildTransaction(
		latestBlock,
		account,
		script,
		cadence.NewArray(hexEncodedTxs),
		coinbaseAddress,
	)
	if err != nil {
		return err
	}

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		return err
	}

	return nil
}

func (t *BatchTxPool) submitSingleTransaction(
	ctx context.Context,
	hexEncodedTx cadence.String,
) error {
	latestBlock, account, err := t.fetchFlowLatestBlockAndCOA(ctx)
	if err != nil {
		return err
	}

	coinbaseAddress, err := cadence.NewString(t.config.Coinbase.Hex())
	if err != nil {
		return err
	}

	script := replaceAddresses(runTxScript, t.config.FlowNetworkID)
	flowTx, err := t.buildTransaction(
		latestBlock,
		account,
		script,
		hexEncodedTx,
		coinbaseAddress,
	)
	if err != nil {
		return err
	}

	if err := t.client.SendTransaction(ctx, *flowTx); err != nil {
		return err
	}

	return nil
}

// buildTransaction creates a Cadence transaction from the provided script,
// with the given arguments and signs it with the configured COA account.
func (t *BatchTxPool) buildTransaction(
	latestBlock *flow.Block,
	account *flow.Account,
	script []byte,
	args ...cadence.Value,
) (*flow.Transaction, error) {
	defer func() {
		t.collector.AvailableSigningKeys(t.keystore.AvailableKeys())
	}()

	flowTx := flow.NewTransaction().
		SetScript(script).
		SetReferenceBlockID(latestBlock.ID).
		SetComputeLimit(flowGo.DefaultMaxTransactionGasLimit)

	for _, arg := range args {
		if err := flowTx.AddArgument(arg); err != nil {
			return nil, fmt.Errorf("failed to add argument: %s, with %w", arg, err)
		}
	}

	// building and signing transactions should be blocking,
	// so we don't have keys conflict
	t.mux.Lock()
	defer t.mux.Unlock()

	accKey, err := t.keystore.Take()
	if err != nil {
		return nil, err
	}

	if err := accKey.SetProposerPayerAndSign(flowTx, account); err != nil {
		accKey.Done()
		return nil, err
	}

	// now that the transaction is prepared, store the transaction's metadata
	accKey.SetLockMetadata(flowTx.ID(), latestBlock.Height)

	t.collector.OperatorBalance(account)

	return flowTx, nil
}
