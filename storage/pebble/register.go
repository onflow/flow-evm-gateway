package pebble

import (
	"encoding/binary"
	"fmt"

	"github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow-go/storage/pebble/registers"

	"github.com/cockroachdb/pebble"
	"github.com/onflow/atree"

	"github.com/onflow/flow-go/fvm/evm/types"
)

var (
	// MinLookupKeyLen defines the minimum length for a valid lookup key
	//
	// Lookup keys use the following format:
	//     [key] / [height]
	// Where:
	// - key: optional variable length field
	// - height: 8 bytes representing the block height (uint64)
	// - separator: '/' is used to separate variable length field
	//
	// Therefore the minimum key would be 1 byte + # of bytes for height
	//     / [height]
	MinLookupKeyLen = 1 + registers.HeightSuffixLen
)

var _ types.BackendStorage = &Register{}

type Register struct {
	store  *Storage
	height uint64
	owner  flow.Address

	batch *pebble.Batch
}

// NewRegister creates a new index instance at the provided height, all reads and
// writes of the registers will happen at that height.
// this is not concurrency safe.
func NewRegister(
	store *Storage,
	height uint64,
	owner flow.Address,
	batch *pebble.Batch,
) *Register {
	return &Register{
		store:  store,
		height: height,
		owner:  owner,

		batch: batch,
	}
}

func (r *Register) GetValue(owner, key []byte) ([]byte, error) {
	if r.owner != flow.BytesToAddress(owner) {
		return nil, fmt.Errorf("owner mismatch. Storage expects a single owner %s, given %s", r.owner.Hex(), flow.BytesToAddress(owner).Hex())
	}

	lookupKey := newLookupKey(r.height, key)
	return r.lookupRegister(lookupKey.Bytes())
}

func (r *Register) SetValue(owner, key, value []byte) error {
	if r.owner != flow.BytesToAddress(owner) {
		return fmt.Errorf("owner mismatch. Storage expects a single owner %s, given %s", r.owner.Hex(), flow.BytesToAddress(owner).Hex())
	}

	encoded := newLookupKey(r.height, key).Bytes()

	var db pebble.Writer = r.store.db
	if r.batch != nil {
		db = r.batch
	}
	err := db.Set(encoded, value, nil)
	if err != nil {
		return fmt.Errorf("failed to set key: %w", err)
	}

	return nil
}

func (r *Register) ValueExists(owner, key []byte) (bool, error) {
	val, err := r.GetValue(owner, key)
	if err != nil {
		return false, err
	}

	return len(val) == 0, nil
}

func (r *Register) AllocateSlabIndex(_ []byte) (atree.SlabIndex, error) {
	return atree.SlabIndexUndefined, fmt.Errorf(
		"unexpected call to allocate slab index",
	)
}

func (r *Register) lookupRegister(key []byte) (flow.RegisterValue, error) {
	var db pebble.Reader = r.store.db
	if r.batch != nil {
		db = r.batch
	}

	iter, err := db.NewIter(&pebble.IterOptions{
		UseL6Filters: true,
	})
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := iter.Close(); err != nil {
			r.store.log.Error().Err(err).Msg("failed to close register iterator")
		}
	}()

	ok := iter.SeekPrefixGE(key)
	if !ok {
		// no such register found (which is equivalent to the register being nil)
		return nil, nil
	}

	binaryValue, err := iter.ValueAndErr()
	if err != nil {
		return nil, fmt.Errorf("failed to get value: %w", err)
	}
	// preventing caller from modifying the iterator's value slices
	valueCopy := make([]byte, len(binaryValue))
	copy(valueCopy, binaryValue)

	return valueCopy, nil
}

// lookupKey is the encoded format of the storage key for looking up register value
type lookupKey struct {
	encoded []byte
}

// Bytes returns the encoded lookup key.
func (h lookupKey) Bytes() []byte {
	return h.encoded
}

// String returns the encoded lookup key as a string.
func (h lookupKey) String() string {
	return string(h.encoded)
}

// newLookupKey takes a height and registerID, returns the key for storing the register value in storage
func newLookupKey(height uint64, key []byte) *lookupKey {
	lookupKey := lookupKey{
		// 1 byte gaps for db prefix and '/' separators
		encoded: make([]byte, 0, MinLookupKeyLen+len(key)),
	}

	// The lookup lookupKey used to find most recent value for a register.
	//
	// The "<lookupKey>" part is the register lookupKey, which is used as a prefix to filter and iterate
	// through updated values at different heights, and find the most recent updated value at or below
	// a certain height.
	lookupKey.encoded = append(lookupKey.encoded, key...)
	lookupKey.encoded = append(lookupKey.encoded, '/')

	// Encode the height getting it to 1s compliment (all bits flipped) and big-endian byte order.
	//
	// Registers are a sparse dataset stored with a single entry per update. To find the value at a particular
	// height, we need to do a scan across the entries to find the highest height that is less than or equal
	// to the target height.
	//
	// Pebble does not support reverse iteration, so we use the height's one's complement to effectively
	// reverse sort on the height. This allows us to use a bitwise forward scan for the next most recent
	// entry.
	onesCompliment := ^height
	lookupKey.encoded = binary.BigEndian.AppendUint64(lookupKey.encoded, onesCompliment)

	return &lookupKey
}
