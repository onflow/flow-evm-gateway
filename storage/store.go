package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/cadence"
)

type BlockExecutedPayload struct {
	Height            uint64
	Hash              string
	TotalSupply       uint64
	ParentBlockHash   string
	ReceiptRoot       string
	TransactionHashes []string
}

func NewBlockExecutedPayload(
	blockExecutedEvent cadence.Event,
) (*BlockExecutedPayload, error) {
	blockExecutedPayload := &BlockExecutedPayload{}

	heightFieldValue := blockExecutedEvent.GetFieldValues()[0]
	heightCadenceValue, ok := heightFieldValue.(cadence.UInt64)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	height := heightCadenceValue.ToGoValue().(uint64)
	blockExecutedPayload.Height = height

	hashFieldValue := blockExecutedEvent.GetFieldValues()[1]
	hashCadenceValue, ok := hashFieldValue.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	hash := hashCadenceValue.ToGoValue().(string)
	blockExecutedPayload.Hash = hash

	totalSupplyFieldValue := blockExecutedEvent.GetFieldValues()[2]
	totalSupplyCadenceValue, ok := totalSupplyFieldValue.(cadence.UInt64)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	totalSupply := totalSupplyCadenceValue.ToGoValue().(uint64)
	blockExecutedPayload.TotalSupply = totalSupply

	parentBlockHashFieldValue := blockExecutedEvent.GetFieldValues()[3]
	parentBlockHashCadenceValue, ok := parentBlockHashFieldValue.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	parentBlockHash := parentBlockHashCadenceValue.ToGoValue().(string)
	blockExecutedPayload.ParentBlockHash = parentBlockHash

	receiptRootFieldValue := blockExecutedEvent.GetFieldValues()[4]
	receiptRootCadenceValue, ok := receiptRootFieldValue.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	receiptRoot := receiptRootCadenceValue.ToGoValue().(string)
	blockExecutedPayload.ReceiptRoot = receiptRoot

	transactionHashesFieldValue := blockExecutedEvent.GetFieldValues()[5]
	transactionHashesCadenceValue, ok := transactionHashesFieldValue.(cadence.Array)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	transactionHashes := make([]string, 0)
	for _, cadenceValue := range transactionHashesCadenceValue.Values {
		transactionHash := cadenceValue.ToGoValue().(string)
		transactionHashes = append(transactionHashes, transactionHash)

	}
	blockExecutedPayload.TransactionHashes = transactionHashes

	return blockExecutedPayload, nil
}

type TransactionExecutedPayload struct {
	BlockHeight             uint64
	TxHash                  string
	Transaction             string
	Failed                  bool
	TxType                  uint8
	GasConsumed             uint64
	DeployedContractAddress string
	ReturnedValue           string
	Logs                    string
}

func NewTransactionExecutedPayload(
	transactionExecutedEvent cadence.Event,
) (*TransactionExecutedPayload, error) {
	transactionExecutedPayload := &TransactionExecutedPayload{}

	blockHeightFieldValue := transactionExecutedEvent.GetFieldValues()[0]
	blockHeightCadenceValue, ok := blockHeightFieldValue.(cadence.UInt64)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	blockHeight := blockHeightCadenceValue.ToGoValue().(uint64)
	transactionExecutedPayload.BlockHeight = blockHeight

	txHashFieldValue := transactionExecutedEvent.GetFieldValues()[1]
	txHashCadenceValue, ok := txHashFieldValue.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	txHash := txHashCadenceValue.ToGoValue().(string)
	transactionExecutedPayload.TxHash = txHash

	transactionFieldValue := transactionExecutedEvent.GetFieldValues()[2]
	transactionCadenceValue, ok := transactionFieldValue.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	transaction := transactionCadenceValue.ToGoValue().(string)
	transactionExecutedPayload.Transaction = transaction

	failedFieldValue := transactionExecutedEvent.GetFieldValues()[3]
	failedCadenceValue, ok := failedFieldValue.(cadence.Bool)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	failed := failedCadenceValue.ToGoValue().(bool)
	transactionExecutedPayload.Failed = failed

	txTypeFieldValue := transactionExecutedEvent.GetFieldValues()[4]
	txTypeCadenceValue, ok := txTypeFieldValue.(cadence.UInt8)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	txType := txTypeCadenceValue.ToGoValue().(uint8)
	transactionExecutedPayload.TxType = txType

	gasConsumedFieldValue := transactionExecutedEvent.GetFieldValues()[5]
	gasConsumedCadenceValue, ok := gasConsumedFieldValue.(cadence.UInt64)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	gasConsumed := gasConsumedCadenceValue.ToGoValue().(uint64)
	transactionExecutedPayload.GasConsumed = gasConsumed

	deployedContractAddressFieldValue := transactionExecutedEvent.GetFieldValues()[6]
	deployedContractAddressCadenceValue, ok := deployedContractAddressFieldValue.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	deployedContractAddress := deployedContractAddressCadenceValue.ToGoValue().(string)
	transactionExecutedPayload.DeployedContractAddress = deployedContractAddress

	returnedValueFieldValue := transactionExecutedEvent.GetFieldValues()[7]
	returnedValueCadenceValue, ok := returnedValueFieldValue.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	returnedValue := returnedValueCadenceValue.ToGoValue().(string)
	transactionExecutedPayload.ReturnedValue = returnedValue

	logsFieldValue := transactionExecutedEvent.GetFieldValues()[8]
	logsCadenceValue, ok := logsFieldValue.(cadence.String)
	if !ok {
		return nil, fmt.Errorf("unable to decode Cadence event")
	}

	logs := logsCadenceValue.ToGoValue().(string)
	transactionExecutedPayload.Logs = logs

	return transactionExecutedPayload, nil
}

type Store struct {
	mu                sync.RWMutex
	logsByTopic       map[string][]*types.Log
	latestHeight      uint64
	accountNonce      map[common.Address]uint64
	blocksByNumber    map[uint64]*BlockExecutedPayload
	txByHash          map[common.Hash]*TransactionExecutedPayload
	blockHashToNumber map[common.Hash]uint64
}

// NewStore returns a new in-memory Store implementation.
// TODO(m-Peter): If `LatestBlockHeight` is called before,
// `StoreBlockHeight`, the called will receive 0. To avoid
// this race condition, we should require an initial value for
// `latestHeight` in `NewStore`.
func NewStore() *Store {
	return &Store{
		accountNonce:      make(map[common.Address]uint64),
		logsByTopic:       make(map[string][]*types.Log),
		txByHash:          make(map[common.Hash]*TransactionExecutedPayload),
		blocksByNumber:    make(map[uint64]*BlockExecutedPayload),
		blockHashToNumber: make(map[common.Hash]uint64),
	}
}

func (s *Store) LatestBlockHeight(ctx context.Context) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.latestHeight, nil
}

func (s *Store) GetAccountNonce(ctx context.Context, address common.Address) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.accountNonce[address]
}

func (s *Store) UpdateAccountNonce(ctx context.Context, event cadence.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	txTypeValue := event.GetFieldValues()[4]
	txTypeCadence, ok := txTypeValue.(cadence.UInt8)
	if !ok {
		return
	}

	txType := txTypeCadence.ToGoValue().(uint8)

	if txType == 255 {
		return
	}

	txEncoded := event.GetFieldValues()[2]
	txEncodedCadence, ok := txEncoded.(cadence.String)
	if !ok {
		return
	}

	txRlpEncoded := txEncodedCadence.ToGoValue().(string)

	decodedTx, err := hex.DecodeString(txRlpEncoded)
	if err != nil {
		panic(err)
	}
	trx := &types.Transaction{}
	encodedLen := uint(len(txRlpEncoded))
	err = trx.DecodeRLP(
		rlp.NewStream(
			bytes.NewReader(decodedTx),
			uint64(encodedLen),
		),
	)
	if err != nil {
		panic(err)
	}
	from, err := types.Sender(types.LatestSignerForChainID(trx.ChainId()), trx)
	if err != nil {
		panic(err)
	}

	s.accountNonce[from] = s.accountNonce[from] + 1
}

func (s *Store) StoreLog(ctx context.Context, event cadence.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	logValue := event.GetFieldValues()[8]
	logC, ok := logValue.(cadence.String)
	if !ok {
		return
	}
	logS := logC.ToGoValue().(string)
	if len(logS) == 0 {
		return
	}
	bt, err := hex.DecodeString(logS)
	if err != nil {
		panic(err)
	}
	logs := []*types.Log{}
	err = rlp.Decode(bytes.NewReader(bt), &logs)
	if err != nil {
		panic(err)
	}
	for _, log := range logs {
		topic := log.Topics[0].Hex()
		s.logsByTopic[topic] = append(s.logsByTopic[topic], logs...)
	}
}

func (s *Store) LogsByTopic(topic string) []*types.Log {
	return s.logsByTopic[topic]
}

func (s *Store) StoreBlock(ctx context.Context, blockPayload cadence.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	blockExecutedPayload, err := NewBlockExecutedPayload(blockPayload)
	if err != nil {
		return err
	}

	s.blocksByNumber[blockExecutedPayload.Height] = blockExecutedPayload
	blockHash := common.HexToHash(blockExecutedPayload.Hash)
	s.blockHashToNumber[blockHash] = blockExecutedPayload.Height
	s.latestHeight = blockExecutedPayload.Height

	return nil
}

func (s *Store) GetBlockByNumber(
	ctx context.Context,
	blockNumber uint64,
) (*BlockExecutedPayload, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	blockExecutedPayload, ok := s.blocksByNumber[blockNumber]
	if !ok {
		return nil, fmt.Errorf("unable to find block for number: %d", blockNumber)
	}

	return blockExecutedPayload, nil
}

func (s *Store) GetBlockByHash(
	ctx context.Context,
	hash common.Hash,
) (*BlockExecutedPayload, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	blockNumber, ok := s.blockHashToNumber[hash]
	if !ok {
		return nil, fmt.Errorf("unable to find block for hash: %s", hash)
	}
	blockExecutedPayload, ok := s.blocksByNumber[blockNumber]
	if !ok {
		return nil, fmt.Errorf("unable to find block for number: %d", blockNumber)
	}

	return blockExecutedPayload, nil
}

func (s *Store) StoreTransaction(
	ctx context.Context,
	transactionPayload cadence.Event,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	transactionExecutedPayload, err := NewTransactionExecutedPayload(transactionPayload)
	if err != nil {
		return err
	}

	txHash := common.HexToHash(transactionExecutedPayload.TxHash)
	s.txByHash[txHash] = transactionExecutedPayload

	return nil
}

func (s *Store) GetTransactionByHash(
	ctx context.Context,
	txHash common.Hash,
) (*TransactionExecutedPayload, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transactionExecutedPayload, ok := s.txByHash[txHash]
	if !ok {
		return nil, fmt.Errorf("unable to find transaction for hash: %s", txHash)
	}

	return transactionExecutedPayload, nil
}
