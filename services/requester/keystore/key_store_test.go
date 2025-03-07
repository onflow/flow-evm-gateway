package keystore

import (
	"errors"
	"sync"
	"testing"
	"time"

	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/test"
	"github.com/onflow/flow-go/utils/unittest"
	"golang.org/x/sync/errgroup"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTake(t *testing.T) {
	keyGenerator := test.AccountKeyGenerator()
	addrGenerator := test.AddressGenerator()

	keys := make([]*AccountKey, 0, 2)
	for range 2 {
		accountKey, signer := keyGenerator.NewWithSigner()
		keys = append(keys, NewAccountKey(*accountKey, addrGenerator.New(), signer))
	}

	ks := New(keys)

	t.Run("Take with no metadata updates", func(t *testing.T) {
		key, err := ks.Take()
		require.NoError(t, err)

		// key is reserved
		assert.Equal(t, 1, ks.AvailableKeys())
		assert.Empty(t, ks.usedKeys)
		assert.True(t, key.inUse.Load())

		key.Done()

		// key is released
		assert.Equal(t, 2, ks.AvailableKeys())
		assert.False(t, key.inUse.Load())
	})

	t.Run("Take with metadata updates", func(t *testing.T) {
		key, err := ks.Take()
		require.NoError(t, err)

		blockHeight := uint64(10)
		txID := identifierFixture()
		key.SetLockMetadata(txID, blockHeight)

		// key is reserved, and metadata is set
		assert.Equal(t, 1, ks.AvailableKeys())
		assert.Len(t, ks.usedKeys, 1)
		assert.True(t, key.inUse.Load())
		assert.Equal(t, blockHeight, key.lastLockedBlock.Load())
		assert.NotNil(t, ks.usedKeys[txID])

		ks.NotifyTransaction(txID)

		// keystore and key should be reset
		assert.Equal(t, 2, ks.AvailableKeys())
		assert.Empty(t, ks.usedKeys)
		assert.False(t, key.inUse.Load())
	})

	t.Run("Take with expiration", func(t *testing.T) {
		key, err := ks.Take()
		require.NoError(t, err)

		blockHeight := uint64(10)
		txID := identifierFixture()
		key.SetLockMetadata(txID, blockHeight)

		// key is reserved, and metadata is set
		assert.Equal(t, 1, ks.AvailableKeys())
		assert.Len(t, ks.usedKeys, 1)
		assert.True(t, key.inUse.Load())
		assert.Equal(t, blockHeight, key.lastLockedBlock.Load())
		assert.NotNil(t, ks.usedKeys[txID])

		// notify for one block before the expiration block, key should still be reserved
		ks.NotifyBlock(blockHeight + accountKeyBlockExpiration - 1)

		assert.True(t, key.inUse.Load())
		assert.NotNil(t, ks.usedKeys[txID])

		// notify for the expiration block
		ks.NotifyBlock(blockHeight + accountKeyBlockExpiration)

		// keystore and key should be reset
		assert.Equal(t, 2, ks.AvailableKeys())
		assert.Empty(t, ks.usedKeys)
		assert.False(t, key.inUse.Load())
	})
}

func TestKeySigning(t *testing.T) {
	keyGenerator := test.AccountKeyGenerator()
	addrGenerator := test.AddressGenerator()

	address := addrGenerator.New()
	accountKey, signer := keyGenerator.NewWithSigner()
	accountKey.Index = 0 // the fixture starts from index 1
	accountKey.SequenceNumber = 42

	ks := New([]*AccountKey{
		NewAccountKey(*accountKey, address, signer),
	})

	key, err := ks.Take()
	require.NoError(t, err)

	account := &sdk.Account{
		Address: address,
		Keys:    []*sdk.AccountKey{accountKey},
	}
	tx := sdk.NewTransaction()

	err = key.SetProposerPayerAndSign(tx, account)
	require.NoError(t, err)

	assert.Equal(t, account.Address, tx.ProposalKey.Address)
	assert.Equal(t, account.Keys[0].Index, tx.ProposalKey.KeyIndex)
	assert.Equal(t, account.Keys[0].SequenceNumber, tx.ProposalKey.SequenceNumber)
	assert.Equal(t, account.Address, tx.Payer)
	assert.NotEmpty(t, tx.EnvelopeSignatures)
}

func TestConcurrentUse(t *testing.T) {
	keyGenerator := test.AccountKeyGenerator()
	addrGenerator := test.AddressGenerator()

	count := uint32(200)
	keys := make([]*AccountKey, 0, count)
	for i := range count {
		accountKey, signer := keyGenerator.NewWithSigner()
		key := NewAccountKey(*accountKey, addrGenerator.New(), signer)
		key.Index = i
		key.SequenceNumber = uint64(i)

		keys = append(keys, key)
	}

	ks := New(keys)

	g := errgroup.Group{}
	g.SetLimit(150)

	wg := sync.WaitGroup{}

	for i := range 10000 {
		g.Go(func() error {
			var key *AccountKey
			for {
				var err error
				key, err = ks.Take()
				if errors.Is(err, ErrNoKeysAvailable) {
					time.Sleep(time.Millisecond)
					continue
				}
				require.NoError(t, err)
				break
			}

			if i%7 == 0 {
				// cause some keys to be released early simulating errors
				key.Done()
				return nil
			}

			// the rest should be released after a delay
			txID := identifierFixture()
			key.SetLockMetadata(txID, uint64(10))

			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(10 * time.Millisecond)
				ks.NotifyTransaction(txID)
			}()

			return nil
		})
	}

	err := g.Wait()
	require.NoError(t, err)

	// all the notifier goroutines are finished by now
	wg.Wait()

	assert.Equal(t, int(count), ks.AvailableKeys(), "all keys should be available")
	assert.Empty(t, ks.usedKeys, "no keys should be in use")
}

func identifierFixture() sdk.Identifier {
	id := unittest.IdentifierFixture()
	return sdk.BytesToID(id[:])
}
