package pebble

import (
	"encoding/binary"
	"fmt"

	"github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow-go/storage/pebble/registers"

	"github.com/cockroachdb/pebble"
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

type RegisterIndex struct {
	store *Storage
	owner flow.Address
}

// NewRegisters creates a new index instance at the provided height, all reads and
// writes of the registers will happen at that height.
// this is not concurrency safe.
//
// The register store does verify that the owner supplied is the one that was used before,
// or that the heights are sequential.
// This should be done by the caller.
func NewRegisters(
	store *Storage,
	owner flow.Address,
) *RegisterIndex {
	return &RegisterIndex{
		store: store,
		owner: owner,
	}
}

// Get returns the register value for the given register ID at the given height.
// Get will check that the owner is the same as the one used to create the index.
func (r *RegisterIndex) Get(id flow.RegisterID, height uint64) (flow.RegisterValue, error) {
	owner := flow.BytesToAddress([]byte(id.Owner))
	if r.owner != flow.BytesToAddress([]byte(id.Owner)) {
		return nil, registerOwnerMismatch(r.owner, owner)
	}

	lookupKey := newLookupKey(height, []byte(id.Key))
	return r.lookupRegister(lookupKey.Bytes())
}

// Store stores the register entries for the given height to the given batch.
// The batch does need to be indexed.
//
// Store will check that all the register entries are for the same owner.
func (r *RegisterIndex) Store(entries flow.RegisterEntries, height uint64, batch *pebble.Batch) error {
	for _, entry := range entries {
		owner := flow.BytesToAddress([]byte(entry.Key.Owner))
		if r.owner != owner {
			return registerOwnerMismatch(r.owner, owner)
		}

		encoded := newLookupKey(height, []byte(entry.Key.Key)).Bytes()

		err := batch.Set(encoded, entry.Value, nil)
		if err != nil {
			return fmt.Errorf("failed to set key: %w", err)
		}
	}

	return nil
}

func (r *RegisterIndex) lookupRegister(key []byte) (flow.RegisterValue, error) {
	db := r.store.db

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
	// RegisterIndex are a sparse dataset stored with a single entry per update. To find the value at a particular
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

func registerOwnerMismatch(expected flow.Address, owner flow.Address) error {
	return fmt.Errorf("owner mismatch. Storage expects a single owner %s, given %s", expected.Hex(), owner.Hex())
}
