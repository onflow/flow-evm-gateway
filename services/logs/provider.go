package logs

import (
	gethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/flow-evm-gateway/storage"
	"math/big"
)

// Provider defines all the log providers no matter the source.
//
// Currently, we have two sources of logs, one is the index storage and the other is from the network stream.
type Provider interface {
	// Get all the logs that match the bloom filter within the start and end block height range.
	// Start and end have special values "latest", if both are set to "latest" that means we want
	// to get all the upcoming new logs that match the bloom filter. If the start and end are
	// defined as anything else we fetch already indexed logs.
	Get(bloom gethTypes.Bloom, start, end *big.Int) (chan []*gethTypes.Log, error)
}

var _ Provider = &StorageProvider{}

type StorageProvider struct {
	receipts storage.ReceiptIndexer
}

func (s StorageProvider) Get(bloom gethTypes.Bloom, start, end *big.Int) chan []*gethTypes.Log {
	s.receipts.BloomsForBlockRange(start, end)
}

var _ Provider = &StreamProvider{}

type StreamProvider struct{}

func (s StreamProvider) Get(bloom gethTypes.Bloom, start, end *big.Int) chan []*gethTypes.Log {
	//TODO implement me
	panic("implement me")
}
