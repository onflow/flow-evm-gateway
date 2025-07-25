package keystore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/testutils"
	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/mocks"
	"github.com/onflow/flow-go-sdk/test"
	"github.com/onflow/flow-go/utils/unittest"
	"github.com/rs/zerolog"
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

	ks := New(
		context.Background(),
		keys,
		testutils.SetupClientForRange(1, 100),
		config.Config{COATxLookupEnabled: true},
		zerolog.Nop(),
	)

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
		assert.Equal(t, key, ks.usedKeys[txID])

		ks.NotifyTransaction(txID)

		// keystore and key should be reset
		assert.Equal(t, 2, ks.AvailableKeys())
		assert.Empty(t, ks.usedKeys)
		assert.False(t, key.inUse.Load())
	})

	t.Run("Take with expiration", func(t *testing.T) {
		blockHeight := uint64(10)
		blockID10 := identifierFixture()
		blockIDNonExpired := identifierFixture()
		client := &testutils.MockClient{
			Client: &mocks.Client{},
			GetTransactionResultsByBlockIDFunc: func(ctx context.Context, blockID sdk.Identifier) ([]*sdk.TransactionResult, error) {
				if blockID == blockID10 {
					return []*sdk.TransactionResult{}, nil
				}

				if blockID == blockIDNonExpired {
					return []*sdk.TransactionResult{
						{
							Status:           sdk.TransactionStatusFinalized,
							Error:            nil,
							Events:           []sdk.Event{},
							BlockID:          blockID,
							BlockHeight:      blockHeight + accountKeyBlockExpiration - 1,
							TransactionID:    identifierFixture(),
							CollectionID:     identifierFixture(),
							ComputationUsage: 104_512,
						},
					}, nil
				}

				return []*sdk.TransactionResult{
					{
						Status:           sdk.TransactionStatusFinalized,
						Error:            nil,
						Events:           []sdk.Event{},
						BlockID:          blockID,
						BlockHeight:      blockHeight + accountKeyBlockExpiration,
						TransactionID:    identifierFixture(),
						CollectionID:     identifierFixture(),
						ComputationUsage: 104_512,
					},
				}, nil
			},
		}
		ks := New(
			context.Background(),
			keys,
			client,
			config.Config{COATxLookupEnabled: true},
			zerolog.Nop(),
		)

		key, err := ks.Take()
		require.NoError(t, err)

		txID := identifierFixture()
		key.SetLockMetadata(txID, blockHeight)

		// key is reserved, and metadata is set
		assert.Equal(t, 1, ks.AvailableKeys())
		assert.Len(t, ks.usedKeys, 1)
		assert.True(t, key.inUse.Load())
		assert.Equal(t, blockHeight, key.lastLockedBlock.Load())
		assert.Equal(t, key, ks.usedKeys[txID])

		// notify for one block before the expiration block, key should still be reserved
		ks.NotifyBlock(
			sdk.BlockHeader{
				ID:     blockIDNonExpired,
				Height: blockHeight + accountKeyBlockExpiration - 1,
			},
		)

		// Give some time to allow the KeyStore to check for the
		// transaction result statuses in the background.
		time.Sleep(time.Second * 2)

		assert.True(t, key.inUse.Load())
		assert.Equal(t, key, ks.usedKeys[txID])

		// notify for the expiration block
		ks.NotifyBlock(
			sdk.BlockHeader{
				ID:     identifierFixture(),
				Height: blockHeight + accountKeyBlockExpiration,
			},
		)

		// Give some time to allow the KeyStore to check for the
		// transaction result statuses in the background.
		time.Sleep(time.Second * 2)

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

	ks := New(
		context.Background(),
		[]*AccountKey{
			NewAccountKey(*accountKey, address, signer),
		},
		testutils.SetupClientForRange(1, 100),
		config.Config{COATxLookupEnabled: true},
		zerolog.Nop(),
	)

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

	// number of keys to add
	keyCount := uint32(100)

	// total tx to send
	totalTxCount := 10_000

	// number of tx to send in parallel
	// should be less than keyCount otherwise the extra workers will just be spinning waiting for
	// keys within the test code.
	concurrentTxCount := 75

	keys := make([]*AccountKey, 0, keyCount)
	for i := range keyCount {
		accountKey, signer := keyGenerator.NewWithSigner()
		key := NewAccountKey(*accountKey, addrGenerator.New(), signer)
		key.Index = i
		key.SequenceNumber = uint64(i)

		keys = append(keys, key)
	}

	ks := New(
		context.Background(),
		keys,
		testutils.SetupClientForRange(1, 100),
		config.Config{COATxLookupEnabled: true},
		zerolog.Nop(),
	)

	g := errgroup.Group{}
	g.SetLimit(concurrentTxCount)

	wg := sync.WaitGroup{}

	for i := range totalTxCount {
		g.Go(func() error {
			var key *AccountKey
			for {
				var err error
				key, err = ks.Take()
				if errors.Is(err, ErrNoKeysAvailable) {
					time.Sleep(100 * time.Microsecond)
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
				time.Sleep(3 * time.Millisecond)
				ks.NotifyTransaction(txID)
			}()

			return nil
		})
	}

	err := g.Wait()
	require.NoError(t, err)

	// all the notifier goroutines are finished by now
	wg.Wait()

	assert.Equal(t, int(keyCount), ks.AvailableKeys(), "all keys should be available")
	assert.Empty(t, ks.usedKeys, "no keys should be in use")
}

func identifierFixture() sdk.Identifier {
	id := unittest.IdentifierFixture()
	return sdk.BytesToID(id[:])
}
