package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-emulator/adapters"
	"github.com/onflow/flow-emulator/emulator"
	"github.com/onflow/flow-emulator/server"
	"github.com/onflow/flow-evm-gateway/services/events"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/memory"
	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go-sdk/crypto"
	evmEmulator "github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/stdlib"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	"github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"math/big"
	"os"
	"strings"
	"time"
)

const testPrivateKey = "61ceacbdce419e25ee8e7c2beceee170a05c9cab1e725a955b15ba94dcd747d2"

var (
	toWei  = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	logger = zerolog.New(os.Stdout)
	sc     = systemcontracts.SystemContractsForChain(flow.Emulator)
)

func startEmulator() (*server.EmulatorServer, error) {
	pkey, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, testPrivateKey)
	if err != nil {
		return nil, err
	}

	genesisToken, err := cadence.NewUFix64("10000.0")
	if err != nil {
		return nil, err
	}

	log := logger.With().Str("component", "emulator").Logger().Level(zerolog.DebugLevel)
	srv := server.NewEmulatorServer(&log, &server.Config{
		ServicePrivateKey:  pkey,
		ServiceKeySigAlgo:  crypto.ECDSA_P256,
		ServiceKeyHashAlgo: crypto.SHA3_256,
		GenesisTokenSupply: genesisToken,
		EVMEnabled:         true,
		WithContracts:      true,
		Host:               "localhost",
	})

	go func() {
		srv.Start()
	}()

	time.Sleep(200 * time.Millisecond) // give it some time to start, dummy but ok for test

	return srv, nil
}

// startEventIngestionEngine will start up the event engine with the grpc subscriber
// listening for events.
// todo for now we return index storage as a way to check the data if it was correctly
// indexed this will be in future replaced with evm gateway API access
func startEventIngestionEngine(ctx context.Context) (
	storage.BlockIndexer,
	storage.ReceiptIndexer,
	storage.TransactionIndexer,
	error,
) {
	client, err := grpc.NewClient("localhost:3569")
	if err != nil {
		return nil, nil, nil, err
	}

	blk, err := client.GetLatestBlock(ctx, false)
	if err != nil {
		return nil, nil, nil, err
	}

	subscriber := events.NewRPCSubscriber(client)
	blocks := memory.NewBlockStorage(memory.WithLatestHeight(blk.Height))
	receipts := memory.NewReceiptStorage()
	txs := memory.NewTransactionStorage()

	log := logger.With().Str("component", "ingestion").Logger()
	engine := events.NewEventIngestionEngine(subscriber, blocks, receipts, txs, log)

	go func() {
		err = engine.Start(ctx)
		if err != nil {
			logger.Error().Err(err)
			panic(err)
		}
	}()

	<-engine.Ready() // wait for engine to be ready
	return blocks, receipts, txs, err
}

// fundEOA funds an evm account provided with the amount.
func fundEOA(
	emu emulator.Emulator,
	flowAmount cadence.UFix64,
	eoaAddress common.Address,
) (*sdk.TransactionResult, error) {
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

	eoaBytes, err := evmHexToCadenceBytes(strings.ReplaceAll(eoaAddress.String(), "0x", ""))
	if err != nil {
		return nil, err
	}

	return flowSendTransaction(emu, code, flowAmount, eoaBytes)
}

// flowSendTransaction sends an evm transaction to the emulator, the code provided doesn't
// have to import EVM, this will be handled by this helper function.
func flowSendTransaction(
	emu emulator.Emulator,
	code string,
	args ...cadence.Value,
) (*sdk.TransactionResult, error) {
	key := emu.ServiceKey()

	codeWrapper := []byte(fmt.Sprintf(`
		import EVM from %s
		import FungibleToken from %s
		import FlowToken from %s

		%s`,
		sc.EVMContract.Address.HexWithPrefix(),
		sc.FungibleToken.Address.HexWithPrefix(),
		sc.FlowToken.Address.HexWithPrefix(),
		code,
	))

	log := logger.With().Str("component", "adapter").Logger().Level(zerolog.DebugLevel)
	adapter := adapters.NewSDKAdapter(&log, emu)

	tx := sdk.NewTransaction().
		SetScript(codeWrapper).
		SetComputeLimit(flow.DefaultMaxTransactionGasLimit).
		SetProposalKey(key.Address, key.Index, key.SequenceNumber).
		SetPayer(key.Address).
		AddAuthorizer(key.Address)

	for _, arg := range args {
		err := tx.AddArgument(arg)
		if err != nil {
			return nil, err
		}
	}

	signer, err := key.Signer()
	if err != nil {
		return nil, err
	}

	err = tx.SignEnvelope(key.Address, key.Index, signer)
	if err != nil {
		return nil, err
	}

	err = adapter.SendTransaction(context.Background(), *tx)
	if err != nil {
		return nil, err
	}

	res, err := adapter.GetTransactionResult(context.Background(), tx.ID())
	if err != nil {
		return nil, err
	}

	return res, nil
}

// evmSignAndRun creates an evm transaction and signs it producing a payload that send using the evmRunTransaction.
func evmSignAndRun(
	emu emulator.Emulator,
	weiValue *big.Int,
	gasLimit uint64,
	signer *ecdsa.PrivateKey,
	nonce uint64,
	to *common.Address,
	data []byte,
) (*sdk.TransactionResult, error) {
	gasPrice := big.NewInt(0)

	evmTx := types.NewTx(&types.LegacyTx{Nonce: nonce, To: to, Value: weiValue, Gas: gasLimit, GasPrice: gasPrice, Data: data})

	signed, err := types.SignTx(evmTx, evmEmulator.GetDefaultSigner(), signer)
	if err != nil {
		return nil, fmt.Errorf("error signing EVM transaction: %w", err)
	}
	var encoded bytes.Buffer
	err = signed.EncodeRLP(&encoded)
	if err != nil {
		return nil, fmt.Errorf("error encoding EVM transaction: %w", err)
	}

	return evmRunTransaction(emu, encoded.Bytes())
}

// evmRunTransaction calls the evm run method with the provided evm signed transaction payload.
func evmRunTransaction(emu emulator.Emulator, signedTx []byte) (*sdk.TransactionResult, error) {
	encodedCadence := make([]cadence.Value, 0)
	for _, b := range signedTx {
		encodedCadence = append(encodedCadence, cadence.UInt8(b))
	}
	transactionBytes := cadence.NewArray(encodedCadence).WithType(stdlib.EVMTransactionBytesCadenceType)

	code := `
	transaction(encodedTx: [UInt8]) {
		prepare(signer: auth(Storage) &Account) {}

		execute {
			let feeAcc <- EVM.createBridgedAccount()
			EVM.run(tx: encodedTx, coinbase: feeAcc.address())
			destroy feeAcc
		}
	}`

	return flowSendTransaction(emu, code, transactionBytes)
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

// todo use types.NewBalanceFromUFix64(evmAmount) when flow-go updated
func flowToWei(flow int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(flow), toWei)
}

type contract struct {
	code    string
	abiJSON string
	a       abi.ABI
}

func newContract(code string, abiJSON string) (*contract, error) {
	a, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, err
	}

	return &contract{
		code:    code,
		abiJSON: abiJSON,
		a:       a,
	}, nil
}

func (c *contract) call(funcName string, args ...any) ([]byte, error) {
	call, err := c.a.Pack(funcName, args...)
	if err != nil {
		return nil, err
	}

	return call, nil
}

func (c *contract) value(name string, data []byte) ([]any, error) {
	return c.a.Unpack(name, data)
}
