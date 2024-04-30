package logs

import (
	"math/big"
	"testing"

	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-evm-gateway/storage/mocks"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
		Return(func(id common.Hash) (*types.Block, error) {
			for _, b := range blocks {
				if mustHash(b).Cmp(id) == 0 {
					return b, nil
				}
			}
			return nil, errors.ErrNotFound
		})

	return blockStorage
}

func receiptStorage() storage.ReceiptIndexer {
	for _, r := range receipts { // calculate bloom filters
		r.Bloom = gethTypes.CreateBloom(gethTypes.Receipts{r})
	}

	receiptStorage := &mocks.ReceiptIndexer{}
	receiptStorage.
		On("GetByBlockHeight", mock.AnythingOfType("*big.Int")).
		Return(func(height *big.Int) ([]*gethTypes.Receipt, error) {
			rcps := make([]*gethTypes.Receipt, 0)
			for _, r := range receipts {
				if r.BlockNumber.Cmp(height) == 0 {
					rcps = append(rcps, r)
				}
			}

			if len(rcps) == 0 {
				return nil, errors.ErrNotFound
			}

			return rcps, nil
		})

	receiptStorage.
		On("BloomsForBlockRange", mock.AnythingOfType("*big.Int"), mock.AnythingOfType("*big.Int")).
		Return(func(start, end *big.Int) ([]*gethTypes.Bloom, []*big.Int, error) {
			blooms := make([]*gethTypes.Bloom, 0)
			heights := make([]*big.Int, 0)

			for _, r := range receipts {
				if r.BlockNumber.Cmp(start) >= 0 && r.BlockNumber.Cmp(end) <= 0 {
					blooms = append(blooms, &r.Bloom)
					heights = append(heights, r.BlockNumber)
				}
			}

			return blooms, heights, nil
		})

	return receiptStorage
}

// todo check if empty address with only topics provided is a valid query

func TestIDFilter(t *testing.T) {
	lgs := receipts[0].Logs
	tests := []struct {
		desc       string
		id         common.Hash
		expectLogs []*gethTypes.Log
		criteria   FilterCriteria
	}{{
		desc: "wildcard all logs",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{lgs[0].Address},
			Topics:    [][]common.Hash{},
		},
		expectLogs: lgs[:2], // first two are all logs from the address
	}, {
		desc: "single topic, single address match single log",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{lgs[0].Address},
			Topics:    [][]common.Hash{lgs[0].Topics[:1]},
		},
		expectLogs: lgs[:1],
	}, {
		desc: "single address no topic match two logs",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{lgs[0].Address},
		},
		expectLogs: lgs[:2],
	}, {
		desc: "single address, both topics match single log",
		id:   mustHash(blocks[0]),
		criteria: FilterCriteria{
			Addresses: []common.Address{lgs[0].Address},
			Topics:    [][]common.Hash{lgs[0].Topics},
		},
		expectLogs: lgs[:1],
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
			filter := NewIDFilter(tt.id, tt.criteria, blockStorage(), receiptStorage())
			logs, err := filter.Match()

			require.NoError(t, err)
			require.Equal(t, tt.expectLogs, logs)
		})
	}
}

func TestRangeFilter(t *testing.T) {
	lgs := [][]*gethTypes.Log{receipts[0].Logs, receipts[1].Logs, receipts[2].Logs, receipts[3].Logs, receipts[4].Logs}

	tests := []struct {
		desc       string
		start, end *big.Int
		expectLogs []*gethTypes.Log
		criteria   FilterCriteria
	}{{
		desc:  "single topic, single address, single block match single log",
		start: big.NewInt(0),
		end:   big.NewInt(1),
		criteria: FilterCriteria{
			Addresses: []common.Address{lgs[0][0].Address},
			Topics:    [][]common.Hash{lgs[0][0].Topics[:1]},
		},
		expectLogs: lgs[0][:1],
	}, {
		desc:  "single topic, single address, all blocks match multiple logs",
		start: big.NewInt(0),
		end:   big.NewInt(4),
		criteria: FilterCriteria{
			Addresses: []common.Address{lgs[0][0].Address},
			Topics:    [][]common.Hash{lgs[0][0].Topics[:1]},
		},
		expectLogs: []*gethTypes.Log{lgs[0][0], lgs[3][1]},
	}, {
		desc:  "single address, all blocks match multiple logs",
		start: big.NewInt(0),
		end:   big.NewInt(4),
		criteria: FilterCriteria{
			Addresses: []common.Address{lgs[0][0].Address},
		},
		expectLogs: []*gethTypes.Log{lgs[0][0], lgs[0][1], lgs[1][0], lgs[3][1]},
	}, {
		desc:  "invalid address, all blocks no match",
		start: big.NewInt(0),
		end:   big.NewInt(4),
		criteria: FilterCriteria{
			Addresses: []common.Address{common.HexToAddress("0x123")},
		},
		expectLogs: []*gethTypes.Log{},
	}, {
		desc:  "single address, non-existing range no match",
		start: big.NewInt(5),
		end:   big.NewInt(10),
		criteria: FilterCriteria{
			Addresses: []common.Address{lgs[0][0].Address},
		},
		expectLogs: []*gethTypes.Log{},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			filter, err := NewRangeFilter(*tt.start, *tt.end, tt.criteria, receiptStorage())
			require.NoError(t, err)

			logs, err := filter.Match()

			require.NoError(t, err)
			require.Equal(t, tt.expectLogs, logs)
		})
	}
}

func TestStreamFilter(t *testing.T) {

}
