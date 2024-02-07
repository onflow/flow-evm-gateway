package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/onflow/cadence"
	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
	"time"
)

const rawAddress = "FACF71692421039876a5BB4F10EF7A439D8ef61E"
const rawPrivateKey = "f6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442"

var toWei = big.NewInt(10000000000)

func TestIntegration_SimpleTransactions(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)

	blocks, _, _, err := startEventIngestionEngine(context.Background())
	require.NoError(t, err)

	emu := srv.Emulator()

	// create a new funded COA and fund an EOA to be used for in tests
	code := `
	transaction(amount: UFix64, eoaAddress: [UInt8; 20]) {
		let fundVault: @FlowToken.Vault
	
		prepare(signer: auth(Storage) &Account) {
			let vaultRef = signer.storage.borrow<auth(FungibleToken.Withdraw) &FlowToken.Vault>(from: /storage/flowTokenVault)
				?? panic("Could not borrow reference to the owner's Vault!")
	
			self.fundVault <- vaultRef.withdraw(amount: amount) as! @FlowToken.Vault
		}
	
		execute {
			let acc <- EVM.createBridgedAccount()
			acc.deposit(from: <-self.fundVault)

			let transferValue = amount - 1.0

			let result = acc.call(
				to: EVM.EVMAddress(bytes: eoaAddress), 
				data: [], 
				gasLimit: 300000, 
				value: EVM.Balance(flow: transferValue)
			)
			
			log(result)
			destroy acc
		}
	}`

	eoaAddress, err := evmHexToCadenceBytes(rawAddress)
	require.NoError(t, err)

	flowAmount, _ := cadence.NewUFix64("5.0")
	evmAmount := big.NewInt(int64((flowAmount.ToGoValue()).(uint64)))
	// todo use types.NewBalanceFromUFix64(evmAmount) when flow-go updated
	weiAmount := evmAmount.Mul(evmAmount, toWei)
	res, err := flowSendTransaction(emu, code, flowAmount, eoaAddress)
	require.NoError(t, err)
	require.NoError(t, res.Error)

	assert.Len(t, res.Events, 6) // 4 evm events + 2 cadence events

	time.Sleep(3 * time.Second)

	// block 1 and 2 should be indexed
	blk, err := blocks.GetByHeight(1)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), blk.Height)
	assert.Equal(t, weiAmount, blk.TotalSupply)
	require.Len(t, blk.TransactionHashes, 1)

	blk, err = blocks.GetByHeight(2)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), blk.Height)
	assert.Equal(t, weiAmount, blk.TotalSupply)
	require.Len(t, blk.TransactionHashes, 1)

}

func evmTransferValue(emu emulator.Emulator, flowAmount *big.Int, signer *ecdsa.PrivateKey, to common.Address) (*sdk.TransactionResult, error) {
	gasPrice := big.NewInt(0)
	weiAmount := flowAmount.Mul(flowAmount, toWei)
	nonce := uint64(1)

	evmTx := types.NewTx(&types.LegacyTx{Nonce: nonce, To: &to, Value: weiAmount, Gas: params.TxGas, GasPrice: gasPrice, Data: nil})

	signed, err := types.SignTx(evmTx, emulator.GetDefaultSigner(), signer)
	if err != nil {
		return nil, fmt.Errorf("error signing EVM transaction: %w", err)
	}
	var encoded bytes.Buffer
	err = signed.EncodeRLP(&encoded)
	if err != nil {
		return nil, fmt.Errorf("error encoding EVM transaction: %w", err)
	}

	encodedCadence := make([]cadence.Value, 0)
	for _, b := range encoded.Bytes() {
		encodedCadence = append(encodedCadence, cadence.UInt8(b))
	}
	transactionBytes := cadence.NewArray(encodedCadence).WithType(stdlib.EVMTransactionBytesCadenceType)

	code := `
	transaction(encodedTx: [UInt8]) {
		execute {
			let feeAcc <- EVM.createBridgedAccount()
			EVM.run(tx: encodedTx, coinbase: feeAcc.address())
			destroy feeAcc
		}
	}`

	return flowSendTransaction(emu, code, transactionBytes)
}

func evmRunTransaction(signedTx []byte) {
	encodedCadence := make([]cadence.Value, 0)
	for _, b := range signedTx {
		encodedCadence = append(encodedCadence, cadence.UInt8(b))
	}
	transactionBytes := cadence.NewArray(encodedCadence).WithType(stdlib.EVMTransactionBytesCadenceType)

	txBody := sdk.NewTransaction().
		SetScript([]byte(fmt.Sprintf(
			`
						import EVM from %s
						import FungibleToken from %s
						import FlowToken from %s
						
						transaction(encodedTx: [UInt8]) {
							execute {
								let feeAcc <- EVM.createBridgedAccount()
								EVM.run(tx: encodedTx, coinbase: feeAcc.address())
								destroy feeAcc
							}
						}
					`,
			sc.EVMContract.Address.HexWithPrefix(),
			sc.FungibleToken.Address.HexWithPrefix(),
			sc.FlowToken.Address.HexWithPrefix(),
		)))

	err := txBody.AddArgument(transactionBytes)
	if err != nil {
		return nil, fmt.Errorf("error adding argument to transaction: %w", err)
	}
	return txBody, nil
}

// evmHexToString takes an evm address as string and convert it to cadence byte array
func evmHexToCadenceBytes(address string) (cadence.Array, error) {
	data, err := hex.DecodeString(address)
	if err != nil {
		return cadence.NewArray(nil), err
	}

	res := make([]cadence.Value, 0)
	for _, d := range data {
		res = append(res, cadence.UInt8(d))
	}
	return cadence.NewArray(res), nil
}
