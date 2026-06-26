package requester

import (
	"sync"

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

	// GetBlockView returns a NonceView over the latest indexed EVM state. A
	// non-nil error is an EXCEPTION, same contract as GetNonce.
	GetBlockView() (NonceView, error)
}

// LocalNonceProvider reads the EOA nonce from the latest height of the
// local state index. It caches the built block view and reuses it while the
// indexed height is unchanged (see GetBlockView).
type LocalNonceProvider struct {
	chainID       flowGo.ChainID
	registerStore *pebble.RegisterStorage
	blocks        storage.BlockIndexer

	// mu guards the cached view below. The cached view is shared across reads;
	// callers that read it concurrently must serialize (the mempool does, via
	// its queueMux).
	mu           sync.Mutex
	cachedView   NonceView
	cachedHeight uint64
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

// GetBlockView returns a NonceView over the latest indexed EVM height. The view
// is cached and reused while the indexed height is unchanged, so a burst of
// reads within one block — many Add calls, or a collectDueBatches pass — builds
// the (expensive) view only once. It is rebuilt when a new block is indexed.
// Reuse is safe because an EOA's on-chain nonce cannot change without a new
// block being indexed.
func (p *LocalNonceProvider) GetBlockView() (NonceView, error) {
	height, err := p.blocks.LatestEVMHeight()
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cachedView != nil && p.cachedHeight == height {
		return p.cachedView, nil
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

	p.cachedView = view
	p.cachedHeight = height

	return view, nil
}

func (p *LocalNonceProvider) GetNonce(address gethCommon.Address) (uint64, error) {
	view, err := p.GetBlockView()
	if err != nil {
		return 0, err
	}

	return view.GetNonce(address)
}
