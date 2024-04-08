package tests

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/onflow/flow-evm-gateway/bootstrap"
	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/stretchr/testify/require"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/goccy/go-json"
	"github.com/gorilla/websocket"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-emulator/adapters"
	"github.com/onflow/flow-emulator/emulator"
	"github.com/onflow/flow-emulator/server"
	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	evmEmulator "github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/stdlib"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	"github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/services/logs"
)

var (
	logger    = zerolog.New(os.Stdout)
	sc        = systemcontracts.SystemContractsForChain(flow.Emulator)
	logOutput = true
)

const (
	sigAlgo           = crypto.SignatureAlgorithm(crypto.ECDSA_P256)
	hashAlgo          = crypto.HashAlgorithm(crypto.SHA3_256)
	servicePrivateKey = "61ceacbdce419e25ee8e7c2beceee170a05c9cab1e725a955b15ba94dcd747d2"
	// this is a test eoa account created on account setup
	eoaTestAddress    = "0xFACF71692421039876a5BB4F10EF7A439D8ef61E"
	eoaTestPrivateKey = "0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442"
	eoaFundAmount     = 5.0
	coaFundAmount     = 10.0
)

func startEmulator(createTestAccounts bool) (*server.EmulatorServer, error) {
	pkey, err := crypto.DecodePrivateKeyHex(sigAlgo, servicePrivateKey)
	if err != nil {
		return nil, err
	}

	genesisToken, err := cadence.NewUFix64("10000.0")
	if err != nil {
		return nil, err
	}

	log := logger.With().Str("component", "emulator").Logger().Level(zerolog.DebugLevel)
	// todo remove
	if !logOutput {
		log = zerolog.Nop()
	}
	srv := server.NewEmulatorServer(&log, &server.Config{
		ServicePrivateKey:      pkey,
		ServiceKeySigAlgo:      sigAlgo,
		ServiceKeyHashAlgo:     hashAlgo,
		GenesisTokenSupply:     genesisToken,
		WithContracts:          true,
		Host:                   "localhost",
		TransactionExpiry:      10,
		TransactionMaxGasLimit: flow.DefaultMaxTransactionGasLimit,
	})

	go func() {
		srv.Start()
	}()

	time.Sleep(1000 * time.Millisecond) // give it some time to start, dummy but ok for test

	if createTestAccounts {
		if err := setupTestAccounts(srv.Emulator()); err != nil {
			return nil, err
		}
	}

	return srv, nil
}

// runWeb3Test will run the test by name, the name
// must match an existing js test file (without the extension)
func runWeb3Test(t *testing.T, name string) {
	stop := servicesSetup(t)
	executeTest(t, name)
	stop()
}

// servicesSetup starts up an emulator and the gateway
// engines required for operation of the evm gateway.
func servicesSetup(t *testing.T) func() {
	srv, err := startEmulator(true)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	emu := srv.Emulator()
	service := emu.ServiceKey()

	// default config
	cfg := &config.Config{
		DatabaseDir:        t.TempDir(),
		AccessNodeGRPCHost: "localhost:3569", // emulator
		RPCPort:            8545,
		RPCHost:            "127.0.0.1",
		FlowNetworkID:      "flow-emulator",
		EVMNetworkID:       evmTypes.FlowEVMTestnetChainID,
		Coinbase:           common.HexToAddress(eoaTestAddress),
		COAAddress:         service.Address,
		COAKey:             service.PrivateKey,
		CreateCOAResource:  false,
		GasPrice:           new(big.Int).SetUint64(0),
		LogLevel:           zerolog.DebugLevel,
		LogWriter:          os.Stdout,
		StreamTimeout:      time.Second * 30,
		StreamLimit:        10,
	}

	if !logOutput {
		cfg.LogWriter = zerolog.Nop()
	}

	go func() {
		err = bootstrap.Start(ctx, cfg)
		require.NoError(t, err)
	}()

	time.Sleep(1 * time.Second) // some time to startup
	return func() {
		cancel()
		srv.Stop()
	}
}

// executeTest will run the provided JS test file using mocha
// and will report failure or success of the test.
func executeTest(t *testing.T, testFile string) {
	command := fmt.Sprintf("./web3js/node_modules/.bin/mocha ./web3js/%s.js", testFile)
	parts := strings.Fields(command)

	t.Run(testFile, func(t *testing.T) {
		cmd := exec.Command(parts[0], parts[1:]...)
		if cmd.Err != nil {
			panic(cmd.Err)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			var exitError *exec.ExitError
			if errors.As(err, &exitError) {
				if exitError.ExitCode() == 1 {
					require.Fail(t, string(out))
				}
				t.Fatalf("unknown test issue: %s, output: %s", err.Error(), string(out))
			}
		}
		t.Log(string(out))
	})
}

// setupTestAccounts creates a service COA and stores it in storage as well as fund it,
// it also funds an EOA test account.
func setupTestAccounts(emu emulator.Emulator) error {
	code := `
	transaction(eoaAddress: [UInt8; 20]) {
		let fundVault: @FlowToken.Vault
		let auth: auth(Storage) &Account
	
		prepare(signer: auth(Storage) &Account) {
			let vaultRef = signer.storage.borrow<auth(FungibleToken.Withdraw) &FlowToken.Vault>(
				from: /storage/flowTokenVault
			) ?? panic("Could not borrow reference to the owner's Vault!")
	
			self.fundVault <- vaultRef.withdraw(amount: 10.0) as! @FlowToken.Vault
			self.auth = signer
		}
	
		execute {
			let account <- EVM.createCadenceOwnedAccount()
			account.deposit(from: <-self.fundVault)
			
			let weiAmount: UInt = 5000000000000000000 // 5 Flow
			let result = account.call(
				to: EVM.EVMAddress(bytes: eoaAddress), 
				data: [], 
				gasLimit: 300000, 
				value: EVM.Balance(attoflow: weiAmount)
			)

			self.auth.storage.save<@EVM.CadenceOwnedAccount>(<-account, to: StoragePath(identifier: "evm")!)
		}
	}`

	eoaBytes, err := evmHexToCadenceBytes(eoaTestAddress)
	if err != nil {
		return err
	}

	res, err := flowSendTransaction(emu, code, eoaBytes)
	if err != nil {
		return err
	}
	if res.Error != nil {
		return res.Error
	}
	return nil
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

	blk, _, err := adapter.GetLatestBlock(context.Background(), true)
	if err != nil {
		return nil, err
	}

	tx := sdk.NewTransaction().
		SetScript(codeWrapper).
		SetComputeLimit(flow.DefaultMaxTransactionGasLimit).
		SetProposalKey(key.Address, key.Index, key.SequenceNumber).
		SetPayer(key.Address).
		SetReferenceBlockID(blk.ID).
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
		let coa: &EVM.CadenceOwnedAccount

		prepare(signer: auth(Storage) &Account) {
			self.coa = signer.storage.borrow<&EVM.CadenceOwnedAccount>(
				from: /storage/evm
			) ?? panic("Could not borrow reference to the COA!")
		}

		execute {
			EVM.run(tx: encodedTx, coinbase: self.coa.address())
		}
	}`

	return flowSendTransaction(emu, code, transactionBytes)
}

// evmHexToString takes an evm address as string and convert it to cadence byte array
func evmHexToCadenceBytes(address string) (cadence.Array, error) {
	address = strings.ReplaceAll(address, "0x", "")
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

// todo refactor rpcTest to use the eth client instead:
// https://github.com/ethereum/go-ethereum/blob/e91cdb49beb4b2a3872b5f2548bf2d6559e4f561/ethclient/ethclient.go#L35
// this will remove the custom code for communication
type rpcTest struct {
	url string
}

// rpcRequest takes url, method (eg. "eth_getBlockByNumber") and params (eg. `["0x03"]` or `[]` if empty)
func (r *rpcTest) request(method string, params string) (json.RawMessage, error) {
	reqURL := fmt.Sprintf(`{"jsonrpc":"2.0","id":0,"method":"%s","params":%s}`, method, params)
	fmt.Println("-> request: ", reqURL)
	body := bytes.NewReader([]byte(reqURL))
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s", r.url), body)
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

func (r *rpcTest) getBlock(height uint64, fullTx bool) (*rpcBlock, error) {
	rpcRes, err := r.request(
		"eth_getBlockByNumber",
		fmt.Sprintf(`["%s",%t]`, uintHex(height), fullTx),
	)
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

func (r *rpcTest) getBlockByHash(hash string, fullTx bool) (*rpcBlock, error) {
	rpcRes, err := r.request(
		"eth_getBlockByHash",
		fmt.Sprintf(`["%s",%t]`, hash, fullTx),
	)
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

func (r *rpcTest) blockNumber() (uint64, error) {
	rpcRes, err := r.request("eth_blockNumber", "[]")
	if err != nil {
		return 0, err
	}

	var blockNumber hexutil.Uint64
	err = json.Unmarshal(rpcRes, &blockNumber)
	if err != nil {
		return 0, err
	}

	return uint64(blockNumber), nil
}

func (r *rpcTest) getTx(hash string) (*api.Transaction, error) {
	rpcRes, err := r.request("eth_getTransactionByHash", fmt.Sprintf(`["%s"]`, hash))
	if err != nil {
		return nil, err
	}

	var txRpc api.Transaction
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

func (r *rpcTest) estimateGas(
	from common.Address,
	data []byte,
	gasLimit uint64,
) (hexutil.Uint64, error) {
	rpcRes, err := r.request(
		"eth_estimateGas",
		fmt.Sprintf(
			`[{"from":"%s","data":"0x%s","gas":"%s"}]`,
			from.Hex(),
			hex.EncodeToString(data),
			hexutil.Uint64(gasLimit),
		),
	)
	if err != nil {
		return hexutil.Uint64(0), err
	}

	var gasUsed hexutil.Uint64
	err = json.Unmarshal(rpcRes, &gasUsed)
	if err != nil {
		return hexutil.Uint64(0), err
	}

	return gasUsed, nil
}

func (r *rpcTest) call(
	to common.Address,
	data []byte,
) ([]byte, error) {
	rpcRes, err := r.request(
		"eth_call",
		fmt.Sprintf(
			`[{"to":"%s","data":"0x%s"}]`,
			to.Hex(),
			hex.EncodeToString(data),
		),
	)
	if err != nil {
		return nil, err
	}

	var result hexutil.Bytes
	err = json.Unmarshal(rpcRes, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (r *rpcTest) getCode(from common.Address) ([]byte, error) {
	rpcRes, err := r.request(
		"eth_getCode",
		fmt.Sprintf(
			`["%s","latest"]`,
			from.Hex(),
		),
	)
	if err != nil {
		return nil, err
	}

	var code hexutil.Bytes
	err = json.Unmarshal(rpcRes, &code)
	if err != nil {
		return nil, err
	}

	return code, nil
}

func (r *rpcTest) newTxFilter() (rpc.ID, error) {
	rpcRes, err := r.request("eth_newPendingTransactionFilter", `[]`)
	if err != nil {
		return "", err
	}

	var id rpc.ID
	err = json.Unmarshal(rpcRes, &id)
	if err != nil {
		return "", err
	}

	return id, nil
}

func (r *rpcTest) newLogsFilter(from string, to string, filter *logs.FilterCriteria) (rpc.ID, error) {
	var topics string
	if len(filter.Topics) > 0 {
		for _, t := range filter.Topics[0] {
			if t == common.HexToHash("0x0") { // if empty replace with null
				topics += "null,"
			} else {
				topics += fmt.Sprintf(`"%s",`, t.String())
			}
		}
		topics = topics[:len(topics)-1] // remove last ,
	}

	params := fmt.Sprintf(`[{
		"fromBlock": "%s",
		"toBlock": "%s",
		"address": "%s",	
		"topics": [
			%s
		]
	}]`, from, to, filter.Addresses[0].Hex(), topics)

	rpcRes, err := r.request("eth_newFilter", params)
	if err != nil {
		return "", err
	}

	var id rpc.ID
	err = json.Unmarshal(rpcRes, &id)
	if err != nil {
		return "", err
	}

	return id, nil
}

func (r *rpcTest) getFilterChangesLogs(id rpc.ID) ([]*types.Log, error) {
	rpcRes, err := r.request("eth_getFilterChanges", fmt.Sprintf(`["%s"]`, id))
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

func (r *rpcTest) getFilterChangesHashes(id rpc.ID) ([]common.Hash, error) {
	rpcRes, err := r.request("eth_getFilterChanges", fmt.Sprintf(`["%s"]`, id))
	if err != nil {
		return nil, err
	}

	var h []common.Hash
	err = json.Unmarshal(rpcRes, &h)
	if err != nil {
		return nil, err
	}

	return h, nil
}

func (r *rpcTest) newBlockFilter() (rpc.ID, error) {
	rpcRes, err := r.request("eth_newBlockFilter", `[]`)
	if err != nil {
		return "", err
	}

	var id rpc.ID
	err = json.Unmarshal(rpcRes, &id)
	if err != nil {
		return "", err
	}

	return id, nil
}

// wsConnect creates a new websocket connection and returns a write and read function
// that can be used to easily write to the stream as well as read the next data.
func (r *rpcTest) wsConnect() (func(string) error, func() (*streamMsg, error), error) {
	u := url.URL{Scheme: "ws", Host: r.url, Path: "/"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, nil, err
	}

	write := func(req string) error {
		fmt.Println("--> ws:", req)
		return c.WriteMessage(websocket.TextMessage, []byte(req))
	}

	read := func() (*streamMsg, error) {
		_, message, err := c.ReadMessage()
		if err != nil {
			return nil, err
		}
		log.Println("<-- ws: ", string(message))

		var msg streamMsg
		err = json.Unmarshal(message, &msg)
		if err != nil {
			return nil, err
		}
		return &msg, nil
	}

	return write, read, nil
}

func newHeadsSubscription() string {
	return `{"jsonrpc":"2.0","id":0,"method":"eth_subscribe","params":["newHeads"]}`
}

func newTransactionsSubscription() string {
	return `{"jsonrpc":"2.0","id":0,"method":"eth_subscribe","params":["newPendingTransactions"]}`
}

func newLogsSubscription(address string, topics string) string {
	return fmt.Sprintf(`
		{
			"jsonrpc": "2.0",
			"id": 0,
			"method": "eth_subscribe",
			"params": ["logs", {"address":"%s", "topics": [%s]}]
		}
	`, address, topics)
}

func unsubscribeRequest(id string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":0,"method":"eth_unsubscribe","params":["%s"]}`, id)
}

func uintHex(x uint64) string {
	return fmt.Sprintf("0x%x", x)
}

type rpcBlock struct {
	Hash         string
	Number       string
	ParentHash   string
	Transactions interface{}
}

func (b *rpcBlock) TransactionHashes() []string {
	txHashes := make([]string, 0)
	switch value := b.Transactions.(type) {
	case []interface{}:
		for _, val := range value {
			switch element := val.(type) {
			case string:
				txHashes = append(txHashes, element)
			case map[string]interface{}:
				txHashes = append(txHashes, element["hash"].(string))
			}
		}
	}
	return txHashes
}

func (b *rpcBlock) FullTransactions() []map[string]interface{} {
	transactions := make([]map[string]interface{}, 0)
	switch value := b.Transactions.(type) {
	case []interface{}:
		for _, val := range value {
			switch element := val.(type) {
			case map[string]interface{}:
				transactions = append(transactions, element)

			}
		}
	}
	return transactions
}

type streamParams struct {
	Subscription string          `json:"subscription"`
	Result       json.RawMessage `json:"result"`
}

type streamMsg struct {
	Jsonrpc string       `json:"jsonrpc"`
	Method  string       `json:"method"`
	Params  streamParams `json:"params"`
	Result  any          `json:"result"`
	ID      any          `json:"id"`
}
