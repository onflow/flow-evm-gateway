package requester

import (
	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/offchain/query"
	flowGo "github.com/onflow/flow-go/model/flow"

	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

// NonceProvider returns the current nonce of the given EOA address.
// The nonce-aware tx pool uses it to determine the expected next nonce.
type NonceProvider interface {
	GetNonce(address gethCommon.Address) (uint64, error)
}

// LocalNonceProvider reads the EOA nonce from the latest height of the
// local state index.
type LocalNonceProvider struct {
	chainID       flowGo.ChainID
	registerStore *pebble.RegisterStorage
	blocks        storage.BlockIndexer
}

var _ NonceProvider = &LocalNonceProvider{}

func NewLocalNonceProvider(
	chainID flowGo.ChainID,
	registerStore *pebble.RegisterStorage,
	blocks storage.BlockIndexer,
) *LocalNonceProvider {
	return &LocalNonceProvider{
		chainID:       chainID,
		registerStore: registerStore,
		blocks:        blocks,
	}
}

func (p *LocalNonceProvider) GetNonce(address gethCommon.Address) (uint64, error) {
	height, err := p.blocks.LatestEVMHeight()
	if err != nil {
		return 0, err
	}

	viewProvider := query.NewViewProvider(
		p.chainID,
		evm.StorageAccountAddress(p.chainID),
		p.registerStore,
		NewOverridableBlocksProvider(p.blocks, p.chainID, nil),
		blockGasLimit,
	)

	view, err := viewProvider.GetBlockView(height)
	if err != nil {
		return 0, err
	}

	return view.GetNonce(address)
}
