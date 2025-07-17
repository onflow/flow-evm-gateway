package keystore

import (
	"context"
	"fmt"
	"sync"

	"github.com/onflow/flow-evm-gateway/config"
	flowsdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access"
	"github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
)

var ErrNoKeysAvailable = fmt.Errorf("no signing keys available")

const accountKeyBlockExpiration = flow.DefaultTransactionExpiry

type KeyLock interface {
	// This method is intended for the happy path of valid EVM transactions.
	// The event subscriber module only subscribes to EVM-related events:
	// - `EVM.TransactionExecuted`
	// - `EVM.BlockExecuted`
	//
	// Valid EVM transactions do emit `EVM.TransactionExecuted` events, so we
	// release the account key that was used by the Flow tx which emitted
	// the above EVM event.
	NotifyTransaction(txID flowsdk.Identifier)
	// This method is intended for the unhappy path of invalid EVM transactions.
	// For each new Flow block, we check the result status of all included Flow
	// transactions, and we release the account keys which they used. This also
	// handles the release of expired transactions, that weren't even included
	// in a Flow block.
	NotifyBlock(blockHeader flowsdk.BlockHeader)
}

type KeyStore struct {
	client        access.Client
	config        config.Config
	availableKeys chan *AccountKey
	usedKeys      map[flowsdk.Identifier]*AccountKey
	size          int
	keyMu         sync.Mutex
	blockChan     chan flowsdk.BlockHeader
	logger        zerolog.Logger

	// Signal channel used to prevent blocking writes
	// on `blockChan` when the node is shutting down.
	done chan struct{}
}

var _ KeyLock = (*KeyStore)(nil)

func New(
	ctx context.Context,
	keys []*AccountKey,
	client access.Client,
	config config.Config,
	logger zerolog.Logger,
) *KeyStore {
	totalKeys := len(keys)

	ks := &KeyStore{
		client:        client,
		config:        config,
		availableKeys: make(chan *AccountKey, totalKeys),
		usedKeys:      map[flowsdk.Identifier]*AccountKey{},
		size:          totalKeys,
		// `KeyStore.NotifyBlock` is called for each new Flow block,
		// so we use a buffered channel to write the new block headers
		// to the `blockChan`, and read them through `processLockedKeys`.
		blockChan: make(chan flowsdk.BlockHeader, 100),
		logger:    logger,
		done:      make(chan struct{}),
	}

	for _, key := range keys {
		key.ks = ks
		ks.availableKeys <- key
	}

	// For cases where the EVM Gateway is run in an index-mode,
	// there is no need to release any keys, since transaction
	// submission is not allowed.
	if ks.size > 0 {
		go ks.processLockedKeys(ctx)
	}

	return ks
}

// AvailableKeys returns the number of keys available for use.
func (k *KeyStore) AvailableKeys() int {
	return len(k.availableKeys)
}

// HasKeysInUse returns whether any of the keys are currently being used.
func (k *KeyStore) HasKeysInUse() bool {
	return k.AvailableKeys() != k.size
}

// Take reserves a key for use in a transaction.
func (k *KeyStore) Take() (*AccountKey, error) {
	select {
	case key := <-k.availableKeys:
		if !key.lock() {
			// this should never happen and means there's a bug
			panic(fmt.Sprintf("key %d available, but locked", key.Index))
		}
		return key, nil
	default:
		return nil, ErrNoKeysAvailable
	}
}

// NotifyTransaction unlocks a key after use and puts it back into the pool.
func (k *KeyStore) NotifyTransaction(txID flowsdk.Identifier) {
	k.keyMu.Lock()
	defer k.keyMu.Unlock()

	k.unsafeUnlockKey(txID)
}

// NotifyBlock is called to notify the KeyStore of a newly ingested block.
// Pending transactions older than a threshold number of blocks are removed.
func (k *KeyStore) NotifyBlock(blockHeader flowsdk.BlockHeader) {
	select {
	case <-k.done:
		k.logger.Warn().Msg(
			"received `NotifyBlock` while the server is shutting down",
		)
	case k.blockChan <- blockHeader:
		k.logger.Info().Msgf(
			"received `NotifyBlock` for block with ID: %s",
			blockHeader.ID,
		)
	}
}

// unsafeUnlockKey unlocks a key referenced by the transaction ID set during setLockMetadata
// the caller must hold the keyMu lock
func (k *KeyStore) unsafeUnlockKey(txID flowsdk.Identifier) {
	if key, ok := k.usedKeys[txID]; ok {
		key.Done()
		delete(k.usedKeys, txID)
	}
}

// release puts a key back into the pool.
func (k *KeyStore) release(key *AccountKey) {
	k.availableKeys <- key
}

// setLockMetadata sets the transaction ID for a key reservation.
// this method is called by the key's SetLockMetadata method
func (k *KeyStore) setLockMetadata(
	key *AccountKey,
	txID flowsdk.Identifier,
) {
	k.keyMu.Lock()
	defer k.keyMu.Unlock()
	k.usedKeys[txID] = key
}

// processLockedKeys reads from the `blockChan` channel, and for each new
// Flow block, it fetches the transaction results of the given block and
// releases the account keys associated with those transactions.
func (k *KeyStore) processLockedKeys(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			close(k.done)
			return
		case blockHeader := <-k.blockChan:
			// Optimization to avoid AN calls when no signing keys have
			// been used. For example, when back-filling the EVM GW state,
			// we don't care about releasing signing keys.
			if !k.HasKeysInUse() {
				continue
			}

			txResults := []*flowsdk.TransactionResult{}
			var err error
			if k.config.COATxLookupEnabled {
				txResults, err = k.client.GetTransactionResultsByBlockID(ctx, blockHeader.ID)
				if err != nil {
					k.logger.Error().Err(err).Msgf(
						"failed to get transaction results for block ID: %s",
						blockHeader.ID.Hex(),
					)
					continue
				}
			}

			k.releasekeys(blockHeader.Height, txResults)
		}
	}
}

// releasekeys accepts a block height and a slice of `TransactionResult`
// objects and releases the account keys used for signing the given
// transactions.
// It also releases the account keys which were last locked more than
// or equal to `accountKeyBlockExpiration` blocks in the past.
func (k *KeyStore) releasekeys(blockHeight uint64, txResults []*flowsdk.TransactionResult) {
	k.keyMu.Lock()
	defer k.keyMu.Unlock()

	for _, txResult := range txResults {
		k.unsafeUnlockKey(txResult.TransactionID)
	}

	for txID, key := range k.usedKeys {
		if blockHeight-key.lastLockedBlock.Load() >= accountKeyBlockExpiration {
			k.unsafeUnlockKey(txID)
		}
	}
}
