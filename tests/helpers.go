package tests

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	evmTypes "github.com/onflow/flow-go/fvm/evm/types"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/bootstrap"

	"github.com/goccy/go-json"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-emulator/adapters"
	"github.com/onflow/flow-emulator/emulator"
	"github.com/onflow/flow-emulator/server"
	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	evmEmulator "github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/systemcontracts"
	"github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
)

var (
	logger         = zerolog.New(zerolog.NewConsoleWriter())
	sc             = systemcontracts.SystemContractsForChain(flow.Emulator)
	logOutput      = os.Getenv("LOG_OUTPUT")
	eoaTestAccount = common.HexToAddress(eoaTestAddress)
)

const (
	sigAlgo           = crypto.SignatureAlgorithm(crypto.ECDSA_P256)
	hashAlgo          = crypto.HashAlgorithm(crypto.SHA3_256)
	servicePrivateKey = "61ceacbdce419e25ee8e7c2beceee170a05c9cab1e725a955b15ba94dcd747d2"
	// this is a test eoa account created on account setup
	eoaTestAddress    = "0xFACF71692421039876a5BB4F10EF7A439D8ef61E"
	eoaTestPrivateKey = "f6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442"
	eoaFundAmount     = 5.0
	coaFundAmount     = 10.0
)

func testLogWriter() io.Writer {
	if logOutput == "false" {
		return zerolog.Nop()
	}

	return zerolog.NewConsoleWriter()
}

func startEmulator(createTestAccounts bool) (*server.EmulatorServer, error) {
	pkey, err := crypto.DecodePrivateKeyHex(sigAlgo, servicePrivateKey)
	if err != nil {
		return nil, err
	}

	genesisToken, err := cadence.NewUFix64("10000.0")
	if err != nil {
		return nil, err
	}

	log := logger.With().Timestamp().Str("component", "emulator").Logger().Level(zerolog.DebugLevel)
	if logOutput == "false" {
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
	_, stop := servicesSetup(t)
	executeTest(t, name)
	stop()
}

func runWeb3TestWithSetup(
	t *testing.T,
	name string,
	setupFunc func(emu emulator.Emulator),
) {
	emu, stop := servicesSetup(t)
	setupFunc(emu)
	executeTest(t, name)
	stop()
}

// servicesSetup starts up an emulator and the gateway
// engines required for operation of the evm gateway.
func servicesSetup(t *testing.T) (emulator.Emulator, func()) {
	srv, err := startEmulator(true)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	emu := srv.Emulator()
	service := emu.ServiceKey()

	// default config
	cfg := &config.Config{
		DatabaseDir:                 t.TempDir(),
		AccessNodeHost:              "localhost:3569", // emulator
		RPCPort:                     8545,
		RPCHost:                     "127.0.0.1",
		FlowNetworkID:               "flow-emulator",
		EVMNetworkID:                evmTypes.FlowEVMPreviewNetChainID,
		Coinbase:                    common.HexToAddress(eoaTestAddress),
		COAAddress:                  service.Address,
		COAKey:                      service.PrivateKey,
		CreateCOAResource:           false,
		GasPrice:                    new(big.Int).SetUint64(0),
		LogLevel:                    zerolog.DebugLevel,
		LogWriter:                   testLogWriter(),
		StreamTimeout:               time.Second * 30,
		StreamLimit:                 10,
		RateLimit:                   50,
		WSEnabled:                   true,
		HashCalculationHeightChange: 0,
	}

	go func() {
		err = bootstrap.Start(ctx, cfg)
		require.NoError(t, err)
	}()

	time.Sleep(2 * time.Second) // some time to startup
	return emu, func() {
		cancel()
		srv.Stop()
	}
}

// executeTest will run the provided JS test file using mocha
// and will report failure or success of the test.
func executeTest(t *testing.T, testFile string) {
	command := fmt.Sprintf(
		"./web3js/node_modules/.bin/mocha ./web3js/%s.js --timeout 120s",
		testFile,
	)
	parts := strings.Fields(command)

	t.Run(testFile, func(t *testing.T) {
		cmd := exec.Command(parts[0], parts[1:]...)
		if cmd.Err != nil {
			t.Log(cmd.Err.Error())
			panic(cmd.Err)
		}

		out, err := cmd.CombinedOutput()
		t.Log(string(out))

		if err != nil {
			var exitError *exec.ExitError
			if errors.As(err, &exitError) {
				if exitError.ExitCode() == 1 {
					require.Fail(t, err.Error())
				}
				t.Fatalf("unknown test issue: %s, output: %s", err.Error(), string(out))
			}
			require.Fail(t, err.Error())
		}
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

	log := logger.With().Timestamp().Str("component", "adapter").Logger().Level(zerolog.DebugLevel)
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

// todo remove this after integration test is refactored
type rpcTest struct {
	url string
}

// rpcRequest takes url, method (eg. "eth_getBlockByNumber") and params (eg. `["0x03"]` or `[]` if empty)
func (r *rpcTest) request(method string, params string) (json.RawMessage, error) {
	reqURL := fmt.Sprintf(`{"jsonrpc":"2.0","id":0,"method":"%s","params":%s}`, method, params)
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
