package logs

import (
	"math/big"
	"testing"

	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
)

var blocks = []*models.Block{
	{Block: &types.Block{Height: 0}},
	{Block: &types.Block{Height: 1}},
	{Block: &types.Block{Height: 2}},
	{Block: &types.Block{Height: 3}},
	{Block: &types.Block{Height: 4}},
	{Block: &types.Block{Height: 5}},
}

func mustHash(b *models.Block) common.Hash {
	h, err := b.Hash()
	if err != nil {
		panic(err)
	}
	return h
}

var receipts = []*models.StorageReceipt{
	{
		BlockNumber: big.NewInt(int64(blocks[0].Height)),
		BlockHash:   mustHash(blocks[0]),
		Logs: []*gethTypes.Log{
			{
				Address: common.BytesToAddress([]byte{0x22}),
				Topics:  []common.Hash{common.HexToHash("aa"), common.HexToHash("bb")},
			},
			{
				Address: common.BytesToAddress([]byte{0x22}),
				Topics:  []common.Hash{common.HexToHash("cc"), common.HexToHash("dd")},
			},
			{
				Address: common.BytesToAddress([]byte{0x33}),
				Topics:  []common.Hash{common.HexToHash("ee"), common.HexToHash("ff")},
			},
		},
	}, {
		BlockNumber: big.NewInt(int64(blocks[1].Height)),
		BlockHash:   mustHash(blocks[1]),
		Logs: []*gethTypes.Log{
			{
				Address: common.BytesToAddress([]byte{0x22}),
				Topics:  []common.Hash{common.HexToHash("cc"), common.HexToHash("11")},
			},
			{
				Address: common.BytesToAddress([]byte{0x55}),
				Topics:  []common.Hash{common.HexToHash("aa"), common.HexToHash("11")},
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
				Topics:  []common.Hash{common.HexToHash("aa"), common.HexToHash("bb"), common.HexToHash("22")},
			},
			{
				Address: common.BytesToAddress([]byte{0x22}),
				Topics:  []common.Hash{common.HexToHash("aa")},
			},
		},
	}, {
		BlockNumber: big.NewInt(int64(blocks[4].Height)),
		BlockHash:   mustHash(blocks[4]),
		Logs: []*gethTypes.Log{
			{
				Address: common.BytesToAddress([]byte{0x88}),
				Topics:  []common.Hash{common.HexToHash("33"), common.HexToHash("44"), common.HexToHash("55")},
			},
		},
	},
}

func blockStorage() storage.BlockIndexer {
	blockStorage := &mocks.BlockIndexer{}
	blockStorage.
		On("GetByID", mock.AnythingOfType("common.Hash")).
		Return(func(id common.Hash) (*models.Block, error) {
			for _, b := range blocks {
				if mustHash(b).Cmp(id) == 0 {
					return b, nil
				}
			}
			return nil, errs.ErrNotFound
		})

	return blockStorage
}

func receiptStorage() storage.ReceiptIndexer {
	for _, r := range receipts { // calculate bloom filters
		rcp := r.ToGethReceipt()
		r.Bloom = gethTypes.CreateBloom(gethTypes.Receipts{rcp})
	}

	receiptStorage := &mocks.ReceiptIndexer{}
	receiptStorage.
		On("GetByBlockHeight", mock.AnythingOfType("uint64")).
		Return(func(height uint64) ([]*models.StorageReceipt, error) {
			rcps := make([]*models.StorageReceipt, 0)
			for _, r := range receipts {
				if r.BlockNumber.Uint64() == height {
					rcps = append(rcps, r)
				}
			}

			if len(rcps) == 0 {
				return nil, errs.ErrNotFound
			}

			return rcps, nil
		})

	receiptStorage.
		On("BloomsForBlockRange", mock.AnythingOfType("uint64"), mock.AnythingOfType("uint64")).
		Return(func(start, end uint64) ([]*models.BloomsHeight, error) {
			blooms := make([]*gethTypes.Bloom, 0)
			bloomsHeight := make([]*models.BloomsHeight, 0)

			for _, r := range receipts {
				if r.BlockNumber.Uint64() >= start && r.BlockNumber.Uint64() <= end {
					blooms = append(blooms, &r.Bloom)
					bloomsHeight = append(bloomsHeight, &models.BloomsHeight{
						Blooms: blooms,
						Height: r.BlockNumber.Uint64(),
					})
				}
			}

			return bloomsHeight, nil
		})

	return receiptStorage
}

func TestIDFilter(t *testing.T) {
	logs := receipts[0].Logs

	tests := []struct {
		desc       string
		id         common.Hash
		expectLogs []*gethTypes.Log
		criteria   FilterCriteria
	}{{
		desc: "wildcard all logs",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{},
			Topics:    [][]common.Hash{},
		},
		expectLogs: logs[:], // block 0 has 3 logs in total
	}, {
		desc: "single topic no address match single log",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{},
			Topics:    [][]common.Hash{logs[0].Topics[:1]},
		},
		expectLogs: logs[:1],
	}, {
		desc: "single out of order topic no address match no logs",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{},
			Topics:    [][]common.Hash{logs[0].Topics[1:]},
		},
		expectLogs: []*gethTypes.Log{},
	}, {
		desc: "single topic with first position wildcard match single log",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{},
			Topics: [][]common.Hash{
				{},
				{logs[0].Topics[1]},
			},
		},
		expectLogs: logs[:1],
	}, {
		desc: "single topic with second position wildcard match single log",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{},
			Topics: [][]common.Hash{
				{logs[0].Topics[0]},
				{},
			},
		},
		expectLogs: logs[:1],
	}, {
		desc: "single topic, single address match single log",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{logs[0].Address},
			Topics:    [][]common.Hash{logs[0].Topics[:1]},
		},
		expectLogs: logs[:1],
	}, {
		desc: "single address no topic match two logs",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{logs[0].Address},
			Topics:    [][]common.Hash{},
		},
		expectLogs: logs[:2],
	}, {
		desc: "single address, both topics match single log",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{logs[0].Address},
			Topics:    [][]common.Hash{logs[0].Topics},
		},
		expectLogs: logs[:1],
	}, {
		desc: "invalid topic match no logs",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Topics: [][]common.Hash{{common.HexToHash("123")}},
		},
		expectLogs: []*gethTypes.Log{},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			filter, err := NewIDFilter(tt.id, tt.criteria, blockStorage(), receiptStorage())
			require.NoError(t, err)

			matchedLogs, err := filter.Match()

			require.NoError(t, err)
			require.Equal(t, tt.expectLogs, matchedLogs)
		})
	}

	t.Run("with topics count exceeding limit", func(t *testing.T) {
		_, err := NewIDFilter(
			common.HexToHash("123"),
			FilterCriteria{
				Topics: [][]common.Hash{
					{common.HexToHash("123")},
					{common.HexToHash("456")},
					{common.HexToHash("789")},
					{common.HexToHash("101")},
					{common.HexToHash("120")},
				},
			},
			blockStorage(),
			receiptStorage(),
		)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"max topics exceeded, only 4 allowed",
		)
	})
}

func TestRangeFilter(t *testing.T) {
	logs := [][]*gethTypes.Log{receipts[0].Logs, receipts[1].Logs, receipts[2].Logs, receipts[3].Logs, receipts[4].Logs}

	tests := []struct {
		desc       string
		start, end uint64
		expectLogs []*gethTypes.Log
		criteria   FilterCriteria
	}{{
		desc:  "single topic, single address, single block match single log",
		start: 0,
		end:   1,
		criteria: FilterCriteria{
			Addresses: []common.Address{logs[0][0].Address},
			Topics:    [][]common.Hash{logs[0][0].Topics[:1]},
		},
		expectLogs: logs[0][:1],
	}, {
		desc:  "single topic, single address, all blocks match multiple logs",
		start: 0,
		end:   4,
		criteria: FilterCriteria{
			Addresses: []common.Address{logs[0][0].Address},
			Topics:    [][]common.Hash{logs[0][0].Topics[:1]},
		},
		expectLogs: []*gethTypes.Log{logs[0][0], logs[3][1]},
	}, {
		desc:  "single address, all blocks match multiple logs",
		start: 0,
		end:   4,
		criteria: FilterCriteria{
			Addresses: []common.Address{logs[0][0].Address},
		},
		expectLogs: []*gethTypes.Log{logs[0][0], logs[0][1], logs[1][0], logs[3][1]},
	}, {
		desc:  "invalid address, all blocks no match",
		start: 0,
		end:   4,
		criteria: FilterCriteria{
			Addresses: []common.Address{common.HexToAddress("0x123")},
		},
		expectLogs: []*gethTypes.Log{},
	}, {
		desc:  "single address, non-existing range no match",
		start: 5,
		end:   10,
		criteria: FilterCriteria{
			Addresses: []common.Address{logs[0][0].Address},
		},
		expectLogs: []*gethTypes.Log{},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			filter, err := NewRangeFilter(tt.start, tt.end, tt.criteria, receiptStorage())
			require.NoError(t, err)

			matchedLogs, err := filter.Match()

			require.NoError(t, err)
			require.Equal(t, tt.expectLogs, matchedLogs)
		})
	}

	t.Run("with topics count exceeding limit", func(t *testing.T) {
		_, err := NewRangeFilter(
			0,
			4,
			FilterCriteria{
				Topics: [][]common.Hash{
					{common.HexToHash("123")},
					{common.HexToHash("456")},
					{common.HexToHash("789")},
					{common.HexToHash("101")},
					{common.HexToHash("120")},
				},
			},
			receiptStorage(),
		)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"max topics exceeded, only 4 allowed",
		)
	})
}

func TestStreamFilter(t *testing.T) {

}
