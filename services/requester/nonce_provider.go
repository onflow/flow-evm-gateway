package requester

import (
	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go/fvm/evm"
	"github.com/onflow/flow-go/fvm/evm/offchain/query"
	flowGo "github.com/onflow/flow-go/model/flow"

	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
)

// NonceView reads EOA nonces at a single, fixed EVM state (one built block
// view). The mempool reads many EOAs' nonces from one view per flush tick
// rather than rebuilding the (expensive) view per address. It is an interface
// so tests can fake it without constructing a real query.View.
type NonceView interface {
	// GetNonce returns the nonce of the given EOA at this view's state.
	GetNonce(address gethCommon.Address) (uint64, error)
}

// NonceProvider returns the current nonce of the given EOA address.
// The transaction mempool uses it to determine the expected next nonce.
type NonceProvider interface {
	// GetNonce returns the current nonce of the given EOA address.
	//
	// A non-nil error represents an EXCEPTION, not an expected condition:
	// the underlying read is a local state-index lookup that should not
	// fail under normal operation. Callers must therefore treat an error
	// as a hard failure (reject the transaction / abort the operation)
	// rather than a routine, recoverable condition to swallow.
	GetNonce(address gethCommon.Address) (uint64, error)

	// GetBlockView builds a NonceView over the latest indexed EVM state. A
	// caller that reads many EOAs' nonces at once (e.g. a single mempool flush
	// tick) can build the view once and reuse it, instead of paying the view
	// build cost per address as GetNonce does. A non-nil error is an
	// EXCEPTION, same contract as GetNonce.
	GetBlockView() (NonceView, error)
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

// GetBlockView builds a NonceView over the latest indexed EVM height.
func (p *LocalNonceProvider) GetBlockView() (NonceView, error) {
	height, err := p.blocks.LatestEVMHeight()
	if err != nil {
		return nil, err
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
		return nil, err
	}

	return view, nil
}

func (p *LocalNonceProvider) GetNonce(address gethCommon.Address) (uint64, error) {
	view, err := p.GetBlockView()
	if err != nil {
		return 0, err
	}

	return view.GetNonce(address)
}
