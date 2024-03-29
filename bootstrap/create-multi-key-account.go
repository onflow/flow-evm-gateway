package bootstrap

import (
	"context"
	"fmt"
	"github.com/onflow/cadence"
	json2 "github.com/onflow/cadence/encoding/json"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go-sdk/templates"
	"golang.org/x/exp/rand"
	"strings"
)

/*
CreateMultiKeyAccount is used to setup an account that can be used with key-rotation mechanism
// todo parts of this are copied from flowkit and go-sdk/templates and should be refactored out once the package are migrated to Cadence 1.0
*/
func CreateMultiKeyAccount(
	client *grpc.Client,
	keyCount int,
	payer flow.Address,
	ftAddress string,
	flowAddress string,
	key crypto.PrivateKey,
) (*flow.Address, []crypto.PrivateKey, error) {

	privKeys := make([]*flow.AccountKey, keyCount)
	pks := make([]crypto.PrivateKey, keyCount)
	for i := 0; i < keyCount; i++ {
		seed := make([]byte, crypto.MinSeedLength)
		_, err := rand.Read(seed)

		pk, err := crypto.GeneratePrivateKey(crypto.ECDSA_P256, seed)
		if err != nil {
			return nil, nil, err
		}

		pks[i] = pk
		privKeys[i] = &flow.AccountKey{
			Index:     i,
			PublicKey: pk.PublicKey(),
			SigAlgo:   crypto.ECDSA_P256,
			HashAlgo:  crypto.SHA3_256,
			Weight:    1000,
		}
	}

	var err error
	keyList := make([]cadence.Value, keyCount)
	for i, key := range privKeys {
		keyList[i], err = templates.AccountKeyToCadenceCryptoKey(key)
		if err != nil {
			return nil, nil, err
		}
	}

	cadencePublicKeys := cadence.NewArray(keyList)
	cadenceContracts := cadence.NewDictionary(nil)

	args := [][]byte{
		json2.MustEncode(cadencePublicKeys),
		json2.MustEncode(cadenceContracts),
	}

	createAndFund = []byte(strings.ReplaceAll(
		string(createAndFund),
		`import "FlowToken"`,
		fmt.Sprintf(`import FlowToken from %s`, flowAddress),
	))
	createAndFund = []byte(strings.ReplaceAll(
		string(createAndFund),
		`import "FungibleToken"`,
		fmt.Sprintf(`import FungibleToken from %s`, ftAddress),
	))

	val, err := cadence.NewUFix64("10.0")
	if err != nil {
		return nil, nil, err
	}
	args = append(args, json2.MustEncode(val))

	tx := flow.NewTransaction().
		SetScript(createAndFund).
		AddAuthorizer(payer)

	for _, arg := range args {
		tx.AddRawArgument(arg)
	}

	blk, err := client.GetLatestBlock(context.Background(), true)
	if err != nil {
		return nil, nil, err
	}

	signer, err := crypto.NewInMemorySigner(key, crypto.SHA3_256)
	if err != nil {
		return nil, nil, err
	}

	acc, err := client.GetAccount(context.Background(), payer)
	if err != nil {
		return nil, nil, err
	}

	var seq uint64
	var index int
	for _, k := range acc.Keys {
		if k.PublicKey.Equals(key.PublicKey()) {
			seq = k.SequenceNumber
			index = k.Index
		}
	}

	err = tx.
		SetPayer(payer).
		SetProposalKey(payer, index, seq).
		SetReferenceBlockID(blk.ID).
		SignEnvelope(payer, index, signer)
	if err != nil {
		return nil, nil, err
	}

	err = client.SendTransaction(context.Background(), *tx)
	if err != nil {
		return nil, nil, err
	}

	res, err := client.GetTransactionResult(context.Background(), tx.ID())
	if err != nil {
		return nil, nil, err
	}
	if res.Error != nil {
		return nil, nil, res.Error
	}

	events := eventsFromTx(res)
	createdAddrs := events.GetCreatedAddresses()

	return createdAddrs[0], pks, nil
}

type flowEvent struct {
	Type   string
	Values map[string]cadence.Value
}

func (e *flowEvent) GetAddress() *flow.Address {
	if a, ok := e.Values["address"].(cadence.Address); ok {
		address := flow.HexToAddress(a.String())
		return &address
	}

	return nil
}

type events []flowEvent

func eventsFromTx(tx *flow.TransactionResult) events {
	var events events
	for _, event := range tx.Events {
		events = append(events, newEvent(event))
	}

	return events
}

func newEvent(event flow.Event) flowEvent {
	var names []string

	for _, eventType := range event.Value.EventType.Fields {
		names = append(names, eventType.Identifier)
	}
	values := make(map[string]cadence.Value)
	for id, field := range event.Value.Fields {
		values[names[id]] = field
	}

	return flowEvent{
		Type:   event.Type,
		Values: values,
	}
}

func (e *events) GetCreatedAddresses() []*flow.Address {
	addresses := make([]*flow.Address, 0)
	for _, event := range *e {
		if event.Type == flow.EventAccountCreated {
			addresses = append(addresses, event.GetAddress())
		}
	}

	return addresses
}

// todo replace once go-sdk/templates and skds scripts are migrated to 1.0

var createAndFund = []byte(`
import Crypto
import "FlowToken"
import "FungibleToken"

transaction(publicKeys: [Crypto.KeyListEntry], contracts: {String: String}, fundAmount: UFix64) {
    let tokenReceiver: &{FungibleToken.Receiver}
    let sentVault: @{FungibleToken.Vault}

	prepare(signer: auth(BorrowValue) &Account) {
		let account = Account(payer: signer)

		// add all the keys to the account
		for key in publicKeys {
			account.keys.add(publicKey: key.publicKey, hashAlgorithm: key.hashAlgorithm, weight: key.weight)
		}

		// add contracts if provided
		for contract in contracts.keys {
			account.contracts.add(name: contract, code: contracts[contract]!.decodeHex())
		}

		self.tokenReceiver = account
          .capabilities.borrow<&{FungibleToken.Receiver}>(/public/flowTokenReceiver)
          ?? panic("Unable to borrow receiver reference")

        let vaultRef = signer.storage.borrow<auth(FungibleToken.Withdraw) &FlowToken.Vault>(from: /storage/flowTokenVault)
            ?? panic("Could not borrow reference to the owner's Vault!")

        self.sentVault <- vaultRef.withdraw(amount: fundAmount)
	}

	execute {
	    self.tokenReceiver.deposit(from: <-self.sentVault)
	}
}
`)
