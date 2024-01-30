package logs

import (
	"bytes"
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

// StorageProvider uses the indexer storage to fetch matching logs.
type StorageProvider struct {
	receipts storage.ReceiptIndexer
}

func (s StorageProvider) Get(bloom gethTypes.Bloom, start, end *big.Int) (chan []*gethTypes.Log, error) {
	rangeBlooms, err := s.receipts.BloomsForBlockRange(start, end)
	if err != nil {
		return nil, err
	}

	logs := make(chan []*gethTypes.Log, 0)
	defer close(logs)

	for height, b := range rangeBlooms {
		// todo add correct bloom matching using composed blooms
		if bytes.Equal(b.Bytes(), bloom.Bytes()) {
			receipt, err := s.receipts.GetByBlockHeight(height)
			if err != nil {
				return nil, err
			}

			logs <- receipt.Logs
		}
	}

	return logs, nil
}

var _ Provider = &StreamProvider{}

// StreamProvider uses stream of logs as they come in to retrieve matching logs.
type StreamProvider struct{}

func (s StreamProvider) Get(bloom gethTypes.Bloom, start, end *big.Int) (chan []*gethTypes.Log, error) {
	//TODO implement me
	panic("implement me")
}
