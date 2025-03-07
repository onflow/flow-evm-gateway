package keystore

import (
	"fmt"

	flowsdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	"go.uber.org/atomic"
)

type AccountKey struct {
	flowsdk.AccountKey

	ks      *KeyStore
	Address flowsdk.Address
	Signer  crypto.Signer

	// lastLockedBlock tracks the block height when this key was last locked
	lastLockedBlock *atomic.Uint64

	// inUse is set to true when the key is locked for use in a transaction
	inUse *atomic.Bool
}

func NewAccountKey(
	accountKey flowsdk.AccountKey,
	address flowsdk.Address,
	signer crypto.Signer,
) *AccountKey {
	return &AccountKey{
		AccountKey:      accountKey,
		Address:         address,
		Signer:          signer,
		lastLockedBlock: atomic.NewUint64(0),
		inUse:           atomic.NewBool(false),
	}
}

// Done releases a key after use.
func (k *AccountKey) Done() {
	// make sure the key is only put back on the availableKeys channel once
	if k.inUse.CompareAndSwap(true, false) {
		k.ks.release(k)
	}
}

// SetLockMetadata sets the transaction ID and reference block height for the transaction the
// key was used for.
func (k *AccountKey) SetLockMetadata(txID flowsdk.Identifier, referenceBlockHeight uint64) {
	k.lastLockedBlock.Store(referenceBlockHeight)
	k.ks.setLockMetadata(k, txID)
}

// SetProposerPayerAndSign sets the proposer, payer, and signs the transaction with the key.
func (k *AccountKey) SetProposerPayerAndSign(
	tx *flowsdk.Transaction,
	account *flowsdk.Account,
) error {
	if k.Address != account.Address {
		return fmt.Errorf(
			"expected address: %v, got address: %v",
			k.Address,
			account.Address,
		)
	}
	if k.Index >= uint32(len(account.Keys)) {
		return fmt.Errorf(
			"key index: %d exceeds keys length: %d",
			k.Index,
			len(account.Keys),
		)
	}
	seqNumber := account.Keys[k.Index].SequenceNumber

	return tx.
		SetProposalKey(k.Address, k.Index, seqNumber).
		SetPayer(k.Address).
		SignEnvelope(k.Address, k.Index, k.Signer)
}

// lock reserves a key for use in a transaction.
// should only be called by the keystore
func (k *AccountKey) lock() bool {
	return k.inUse.CompareAndSwap(false, true)
}
