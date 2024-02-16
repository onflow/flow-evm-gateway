package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/goccy/go-json"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-emulator/adapters"
	"github.com/onflow/flow-emulator/emulator"
	"github.com/onflow/flow-emulator/server"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/services/ingestion"
	"github.com/onflow/flow-evm-gateway/services/logs"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-evm-gateway/storage/pebble"
	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go-sdk/crypto"
	evmEmulator "github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/stdlib"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	"github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
	"io"
	"math/big"
	"net/http"
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
func startEventIngestionEngine(ctx context.Context, dbDir string) (
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

	subscriber := ingestion.NewRPCSubscriber(client)

	log := logger.With().Str("component", "database").Logger()
	db, err := pebble.New(dbDir, log)
	if err != nil {
		return nil, nil, nil, err
	}

	blocks, err := pebble.NewBlocks(db, pebble.WithInitHeight(blk.Height))
	receipts := pebble.NewReceipts(db)
	accounts := pebble.NewAccounts(db)
	txs := pebble.NewTransactions(db)

	log = logger.With().Str("component", "ingestion").Logger()
	engine := ingestion.NewEventIngestionEngine(subscriber, blocks, receipts, txs, accounts, log)

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

func evmSign(
	weiValue *big.Int,
	gasLimit uint64,
	signer *ecdsa.PrivateKey,
	nonce uint64,
	to *common.Address,
	data []byte) ([]byte, common.Hash, error) {
	gasPrice := big.NewInt(0)

	evmTx := types.NewTx(&types.LegacyTx{Nonce: nonce, To: to, Value: weiValue, Gas: gasLimit, GasPrice: gasPrice, Data: data})

	signed, err := types.SignTx(evmTx, evmEmulator.GetDefaultSigner(), signer)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("error signing EVM transaction: %w", err)
	}
	var encoded bytes.Buffer
	err = signed.EncodeRLP(&encoded)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("error encoding EVM transaction: %w", err)
	}

	return encoded.Bytes(), signed.Hash(), nil
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
) (*sdk.TransactionResult, common.Hash, error) {
	signed, evmID, err := evmSign(weiValue, gasLimit, signer, nonce, to, data)
	if err != nil {
		return nil, common.Hash{}, err
	}

	res, err := evmRunTransaction(emu, signed)
	if err != nil {
		return nil, common.Hash{}, err
	}

	return res, evmID, nil
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

type rpcTest struct {
	url string
}

// rpcRequest takes url, method (eg. "eth_getBlockByNumber") and params (eg. `["0x03"]` or `[]` if empty)
func (r *rpcTest) request(method string, params string) (json.RawMessage, error) {
	reqURL := fmt.Sprintf(`{"jsonrpc":"2.0","id":0,"method":"%s","params":%s}`, method, params)
	fmt.Println("-> request: ", reqURL)
	body := bytes.NewReader([]byte(reqURL))
	req, err := http.NewRequest(http.MethodPost, r.url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept-encoding", "identity")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	content, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	fmt.Println("<- result: ", string(content))

	type rpcResult struct {
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}

	var resp rpcResult
	err = json.Unmarshal(content, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s", resp.Error)
	}

	return resp.Result, nil
}

func (r *rpcTest) getLogs(
	hash *common.Hash,
	from *big.Int,
	to *big.Int,
	filter *logs.FilterCriteria,
) ([]*types.Log, error) {
	var id, ranges, topics string

	for _, t := range filter.Topics[0] {
		topics += fmt.Sprintf(`"%s",`, t.String())
	}
	topics = topics[:len(topics)-1] // remove last ,

	if hash != nil {
		id = fmt.Sprintf(`"blockHash": "%s",`, hash.Hex())
	} else {
		ranges = fmt.Sprintf(`
			"fromBlock": "0x%x",
			"toBlock": "0x%x",
		`, from, to)
	}

	params := fmt.Sprintf(`[{
		%s
		%s
		"address": "%s",	
		"topics": [
			%s
		]
	}]`, ranges, id, filter.Addresses[0].Hex(), topics)

	rpcRes, err := r.request("eth_getLogs", params)
	if err != nil {
		return nil, err
	}

	var lg []*types.Log
	err = json.Unmarshal(rpcRes, &lg)
	if err != nil {
		return nil, err
	}

	return lg, nil
}

func (r *rpcTest) getBlock(height uint64) (*rpcBlock, error) {
	rpcRes, err := r.request("eth_getBlockByNumber", fmt.Sprintf(`["%s",false]`, uintHex(height)))
	if err != nil {
		return nil, err
	}

	var blkRpc rpcBlock
	err = json.Unmarshal(rpcRes, &blkRpc)
	if err != nil {
		return nil, err
	}

	return &blkRpc, nil
}

func (r *rpcTest) getTx(hash string) (*api.RPCTransaction, error) {
	rpcRes, err := r.request("eth_getTransactionByHash", fmt.Sprintf(`["%s"]`, hash))
	if err != nil {
		return nil, err
	}

	var txRpc api.RPCTransaction
	err = json.Unmarshal(rpcRes, &txRpc)
	if err != nil {
		return nil, err
	}

	return &txRpc, nil
}

func (r *rpcTest) getReceipt(hash string) (*types.Receipt, error) {
	rpcRes, err := r.request("eth_getTransactionReceipt", fmt.Sprintf(`["%s"]`, hash))
	if err != nil {
		return nil, err
	}

	var rcp types.Receipt
	err = json.Unmarshal(rpcRes, &rcp)
	if err != nil {
		return nil, err
	}

	return &rcp, nil
}

func (r *rpcTest) sendRawTx(signed []byte) (common.Hash, error) {
	rpcRes, err := r.request("eth_sendRawTransaction", fmt.Sprintf(`["0x%x"]`, signed))
	if err != nil {
		return common.Hash{}, err
	}

	var h common.Hash
	err = json.Unmarshal(rpcRes, &h)
	if err != nil {
		return common.Hash{}, err
	}

	return h, nil
}

func (r *rpcTest) getNonce(address common.Address) (uint64, error) {
	rpcRes, err := r.request("eth_getTransactionCount", fmt.Sprintf(`["%s"]`, address.Hex()))
	if err != nil {
		return 0, err
	}

	var u hexutil.Uint64
	err = json.Unmarshal(rpcRes, &u)
	if err != nil {
		return 0, err
	}

	return uint64(u), nil
}

func (r *rpcTest) getBalance(address common.Address) (*hexutil.Big, error) {
	rpcRes, err := r.request("eth_getBalance", fmt.Sprintf(`["%s", "latest"]`, address.Hex()))
	if err != nil {
		return nil, err
	}

	var u hexutil.Big
	err = json.Unmarshal(rpcRes, &u)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

func uintHex(x uint64) string {
	return fmt.Sprintf("0x%x", x)
}

type rpcBlock struct {
	Hash         string
	Number       string
	ParentHash   string
	Transactions []string
}

type rpcTx struct {
	BlockHash   string
	BlockNumber string
	Gas         string
	GasPrice    string
	From        string
	Hash        string
	Input       hexutil.Bytes
	Nonce       string
	Type        string
}
