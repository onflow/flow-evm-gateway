package logs

import (
	"github.com/ethereum/go-ethereum/common"
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

var blocks = []*types.Block{
	{Height: 0}, {Height: 1}, {Height: 2}, {Height: 3}, {Height: 4}, {Height: 5},
}

func mustHash(b *types.Block) common.Hash {
	h, err := b.Hash()
	if err != nil {
		panic(err)
	}
	return h
}

var receipts = []*gethTypes.Receipt{
	{
		BlockNumber: big.NewInt(int64(blocks[0].Height)),
		BlockHash:   mustHash(blocks[0]),
		Logs: []*gethTypes.Log{
			{
				Address: common.BytesToAddress([]byte{0x22}),
				Topics:  []common.Hash{common.HexToHash("alfa"), common.HexToHash("bravo")},
			},
			{
				Address: common.BytesToAddress([]byte{0x22}),
				Topics:  []common.Hash{common.HexToHash("charlie"), common.HexToHash("delta")},
			},
			{
				Address: common.BytesToAddress([]byte{0x33}),
				Topics:  []common.Hash{common.HexToHash("echo"), common.HexToHash("foxtrot")},
			},
		},
	}, {
		BlockNumber: big.NewInt(int64(blocks[1].Height)),
		BlockHash:   mustHash(blocks[1]),
		Logs: []*gethTypes.Log{
			{
				Address: common.BytesToAddress([]byte{0x22}),
				Topics:  []common.Hash{common.HexToHash("charlie"), common.HexToHash("hotel")},
			},
			{
				Address: common.BytesToAddress([]byte{0x55}),
				Topics:  []common.Hash{common.HexToHash("alfa"), common.HexToHash("hotel")},
			},
		},
	}, {
		BlockNumber: big.NewInt(int64(blocks[2].Height)),
		BlockHash:   mustHash(blocks[2]),
		Logs:        []*gethTypes.Log{},
	}, {
		BlockNumber: big.NewInt(int64(blocks[3].Height)),
		BlockHash:   mustHash(blocks[3]),
		Logs: []*gethTypes.Log{
			{
				Address: common.BytesToAddress([]byte{0x66}),
				Topics:  []common.Hash{common.HexToHash("alfa"), common.HexToHash("bravo"), common.HexToHash("india")},
			},
			{
				Address: common.BytesToAddress([]byte{0x22}),
				Topics:  []common.Hash{common.HexToHash("alfa")},
			},
		},
	}, {
		BlockNumber: big.NewInt(int64(blocks[4].Height)),
		BlockHash:   mustHash(blocks[4]),
		Logs: []*gethTypes.Log{
			{
				Address: common.BytesToAddress([]byte{0x88}),
				Topics:  []common.Hash{common.HexToHash("juliet"), common.HexToHash("kilo"), common.HexToHash("lima")},
			},
		},
	},
}

func blockStorage() storage.BlockIndexer {
	blockStorage := &mocks.BlockIndexer{}
	blockStorage.
		On("GetByID", mock.AnythingOfType("common.Hash")).
		Return(func(id common.Hash) (*types.Block, error) {
			for _, b := range blocks {
				if mustHash(b).Cmp(id) == 0 {
					return b, nil
				}
			}
			return nil, errors.NotFound
		})

	return blockStorage
}

func receiptStorage() storage.ReceiptIndexer {
	receiptStorage := &mocks.ReceiptIndexer{}
	receiptStorage.
		On("GetByBlockHeight", mock.AnythingOfType("*big.Int")).
		Return(func(height *big.Int) (*gethTypes.Receipt, error) {
			for _, r := range receipts {
				if r.BlockNumber.Cmp(height) == 0 {
					return r, nil
				}
			}
			return nil, errors.NotFound
		})

	return receiptStorage
}

func TestIDFilter(t *testing.T) {
	blockStorage := blockStorage()
	receiptStorage := receiptStorage()

	t.Run("single topic, single address", func(t *testing.T) {
		id, _ := blocks[0].Hash()
		log := receipts[0].Logs[0]
		criteria := FilterCriteria{
			Addresses: []common.Address{log.Address},
			Topics:    [][]common.Hash{{log.Topics[0]}},
		}
		filter := NewIDFilter(id, criteria, blockStorage, receiptStorage)
		logs, err := filter.Match()

		require.NoError(t, err)
		require.Len(t, logs, 1)
		require.Equal(t, logs[0], log)
	})
}

func TestRangeFilter(t *testing.T) {

}

func TestStreamFilter(t *testing.T) {

}
