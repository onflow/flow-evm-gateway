package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/cadence"
)

type Store struct {
	mu           sync.RWMutex
	logsByTopic  map[string][]*types.Log
	latestHeight uint64
	accountNonce map[common.Address]uint64
}

// NewStore returns a new in-memory Store implementation.
// TODO(m-Peter): If `LatestBlockHeight` is called before,
// `StoreBlockHeight`, the called will receive 0. To avoid
// this race condition, we should require an initial value for
// `latestHeight` in `NewStore`.
func NewStore() *Store {
	return &Store{
		accountNonce: make(map[common.Address]uint64),
		logsByTopic:  make(map[string][]*types.Log),
	}
}

func (s *Store) LatestBlockHeight(ctx context.Context) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.latestHeight, nil
}

func (s *Store) GetAccountNonce(ctx context.Context, address common.Address) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()

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

func (s *Store) StoreBlockHeight(ctx context.Context, blockHeight uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.storeBlockHeight(blockHeight)
}

func (s *Store) storeBlockHeight(blockHeight uint64) error {
	if blockHeight > s.latestHeight {
		s.latestHeight = blockHeight
	}

	return nil
}
