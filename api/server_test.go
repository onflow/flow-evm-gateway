package api_test

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/api/mocks"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/grpc"
	sdkCrypto "github.com/onflow/flow-go-sdk/crypto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

//go:embed fixtures/eth_json_rpc_requests.json
var requests string

//go:embed fixtures/eth_json_rpc_responses.json
var responses string

var mockFlowClient = new(mocks.MockAccessClient)

func TestServerJSONRPCOveHTTPHandler(t *testing.T) {
	store := storage.NewStore()
	srv := api.NewHTTPServer(zerolog.Logger{}, rpc.DefaultHTTPTimeouts)
	config := &api.Config{
		ChainID:  api.FlowEVMTestnetChainID,
		Coinbase: common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"),
		GasPrice: api.DefaultGasPrice,
	}
	blockchainAPI := api.NewBlockChainAPI(config, store, mockFlowClient)
	supportedAPIs := api.SupportedAPIs(blockchainAPI)
	srv.EnableRPC(supportedAPIs)
	srv.SetListenAddr("localhost", 8545)
	err := srv.Start()
	defer srv.Stop()
	if err != nil {
		panic(err)
	}

	url := "http://" + srv.ListenAddr() + "/rpc"

	expectedResponses := strings.Split(responses, "\n")
	for i, request := range strings.Split(requests, "\n") {
		resp := rpcRequest(url, request, "origin", "test.com")
		defer resp.Body.Close()
		content, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		expectedResponse := expectedResponses[i]

		assert.Equal(t, expectedResponse, strings.TrimSuffix(string(content), "\n"))
	}

	t.Run("eth_getBlockByNumber", func(t *testing.T) {
		request := `{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x1",false]}`
		expectedResponse := `{"jsonrpc":"2.0","id":1,"result":{"difficulty":"0x4ea3f27bc","extraData":"0x476574682f4c5649562f76312e302e302f6c696e75782f676f312e342e32","gasLimit":"0x1388","gasUsed":"0x0","hash":"0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9","logsBloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","miner":"0xbb7b8287f3f0a933474a79eae42cbca977791171","mixHash":"0x4fffe9ae21f1c9e15207b1f472d5bbdd68c9595d461666602f2be20daf5e7843","nonce":"0x689056015818adbe","number":"0x1","parentHash":"0xe81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421c0","receiptsRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","sha3Uncles":"0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347","size":"0x220","stateRoot":"0xddc8b0234c2e0cad087c8b389aa7ef01f7d79b2570bccb77ce48648aa61c904d","timestamp":"0x55ba467c","totalDifficulty":"0x78ed983323d","transactions":["0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9"],"transactionsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","uncles":[]}}`

		event := blockExecutedEvent(
			1,
			"0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9",
			7766279631452241920,
			"0xe81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421c0",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
			[]string{"0xf31ee13dad8f38431fd31278b12be62e6b77e6923f0b7a446eb1affb61f21fc9"},
		)
		err := store.StoreBlock(context.Background(), event)
		require.NoError(t, err)

		resp := rpcRequest(url, request, "origin", "test.com")
		defer resp.Body.Close()

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		assert.Equal(t, expectedResponse, strings.TrimSuffix(string(content), "\n"))
	})

	t.Run("eth_getTransactionReceipt", func(t *testing.T) {
		request := `{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c"]}`
		expectedResponse := `{"jsonrpc":"2.0","id":1,"result":{"blockHash":"0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2","blockNumber":"0x3","contractAddress":"0x0000000000000000000000000000000000000000","cumulativeGasUsed":"0xc350","effectiveGasPrice":"0x4a817c800","from":"0x658bdf435d810c91414ec09147daa6db62406379","gasUsed":"0x57f2","logs":[{"address":"0x99466ed2e37b892a2ee3e9cd55a98b68f5735db2","topics":["0x24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da"],"data":"0x000000000000000000000000000000000000000000000000000000000000002a","blockNumber":"0x0","transactionHash":"0x0000000000000000000000000000000000000000000000000000000000000000","transactionIndex":"0x0","blockHash":"0x0000000000000000000000000000000000000000000000000000000000000000","logIndex":"0x0","removed":false}],"logsBloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000020000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000004000000","status":"0x1","to":"0x99466ed2e37b892a2ee3e9cd55a98b68f5735db2","transactionHash":"0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c","transactionIndex":"0x0","type":"0x2"}}`

		event := transactionExecutedEvent(
			3,
			"0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c",
			"b88c02f88982029a01808083124f809499466ed2e37b892a2ee3e9cd55a98b68f5735db280a4c6888fa10000000000000000000000000000000000000000000000000000000000000006c001a0f84168f821b427dc158c4d8083bdc4b43e178cf0977a2c5eefbcbedcc4e351b0a066a747a38c6c266b9dc2136523cef04395918de37773db63d574aabde59c12eb",
			false,
			2,
			22514,
			"0000000000000000000000000000000000000000",
			"000000000000000000000000000000000000000000000000000000000000002a",
			"f85af8589499466ed2e37b892a2ee3e9cd55a98b68f5735db2e1a024abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503daa0000000000000000000000000000000000000000000000000000000000000002a",
		)

		err := store.StoreTransaction(context.Background(), event)
		require.NoError(t, err)

		resp := rpcRequest(url, request, "origin", "test.com")
		defer resp.Body.Close()

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		assert.Equal(t, expectedResponse, strings.TrimSuffix(string(content), "\n"))
	})

	t.Run("eth_call", func(t *testing.T) {
		request := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[{"from":"0xb60e8dd61c5d32be8058bb8eb970870f07233155","to":"0xd46e8dd67c5d32be8058bb8eb970870f07244567","gas":"0x76c0","gasPrice":"0x9184e72a000","value":"0x9184e72a","input":"0xd46e8dd67c5d32be8d46e8dd67c5d32be8058bb8eb970870f072445675058bb8eb970870f072445675"}]}`
		expectedResponse := `{"jsonrpc":"2.0","id":1,"result":"0x000000000000000000000000000000000000000000000000000000000000002a"}`

		result, err := hex.DecodeString("000000000000000000000000000000000000000000000000000000000000002a")
		require.NoError(t, err)
		toBytes := make([]cadence.Value, 0)
		for _, bt := range result {
			toBytes = append(toBytes, cadence.UInt8(bt))
		}
		returnValue := cadence.NewArray(
			toBytes,
		).WithType(cadence.NewVariableSizedArrayType(cadence.TheUInt8Type))
		mockFlowClient.On(
			"ExecuteScriptAtLatestBlock",
			mock.Anything,
			api.BridgedAccountCall,
			mock.Anything,
		).Once().Return(returnValue, nil)

		blockchainAPI = api.NewBlockChainAPI(config, store, mockFlowClient)

		resp := rpcRequest(url, request, "origin", "test.com")
		defer resp.Body.Close()

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		assert.Equal(t, expectedResponse, strings.TrimSuffix(string(content), "\n"))
	})

	t.Run("eth_sendRawTransaction", func(t *testing.T) {
		request := `{"jsonrpc":"2.0","id":1,"method":"eth_sendRawTransaction","params":["0xb88c02f88982029a01808083124f809499466ed2e37b892a2ee3e9cd55a98b68f5735db280a4c6888fa10000000000000000000000000000000000000000000000000000000000000006c001a0f84168f821b427dc158c4d8083bdc4b43e178cf0977a2c5eefbcbedcc4e351b0a066a747a38c6c266b9dc2136523cef04395918de37773db63d574aabde59c12eb"]}`
		expectedResponse := `{"jsonrpc":"2.0","id":1,"result":"0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c"}`

		blockchainAPI = api.NewBlockChainAPI(config, store, mockFlowClient)

		block := &flow.Block{
			BlockHeader: flow.BlockHeader{
				ID: flow.EmptyID,
			},
		}
		mockFlowClient.On("GetLatestBlock", mock.Anything, mock.Anything).Return(block, nil)

		privateKey, err := sdkCrypto.DecodePrivateKeyHex(sdkCrypto.ECDSA_P256, strings.Replace("2619878f0e2ff438d17835c2a4561cb87b4d24d72d12ec34569acd0dd4af7c21", "0x", "", 1))
		require.NoError(t, err)
		key := &flow.AccountKey{
			Index:          0,
			PublicKey:      privateKey.PublicKey(),
			SigAlgo:        privateKey.Algorithm(),
			HashAlgo:       sdkCrypto.SHA3_256,
			Weight:         1000,
			SequenceNumber: uint64(0),
			Revoked:        false,
		}
		account := &flow.Account{
			Address: flow.HexToAddress("0xf8d6e0586b0a20c7"),
			Keys:    []*flow.AccountKey{key},
		}
		mockFlowClient.On("GetAccount", mock.Anything, mock.Anything).Return(account, nil)

		mockFlowClient.On("SendTransaction", mock.Anything, mock.Anything).Return(nil)

		resp := rpcRequest(url, request, "origin", "test.com")
		defer resp.Body.Close()

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		assert.Equal(t, expectedResponse, strings.TrimSuffix(string(content), "\n"))
	})

	t.Run("eth_getTransactionByHash", func(t *testing.T) {
		request := `{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionByHash","params":["0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c"]}`
		expectedResponse := `{"jsonrpc":"2.0","id":1,"result":{"blockHash":"0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2","blockNumber":"0x3","from":"0xa7d9ddbe1f17865597fbd27ec712455208b6b76d","gas":"0x57f2","gasPrice":"0x0","hash":"0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c","input":"0xc6888fa10000000000000000000000000000000000000000000000000000000000000006","nonce":"0x1","to":"0x99466ed2e37b892a2ee3e9cd55a98b68f5735db2","transactionIndex":"0x0","value":"0x0","type":"0x2","v":"0x1","r":"0xf84168f821b427dc158c4d8083bdc4b43e178cf0977a2c5eefbcbedcc4e351b0","s":"0x66a747a38c6c266b9dc2136523cef04395918de37773db63d574aabde59c12eb"}}`

		event := transactionExecutedEvent(
			3,
			"0xb47d74ea64221eb941490bdc0c9a404dacd0a8573379a45c992ac60ee3e83c3c",
			"b88c02f88982029a01808083124f809499466ed2e37b892a2ee3e9cd55a98b68f5735db280a4c6888fa10000000000000000000000000000000000000000000000000000000000000006c001a0f84168f821b427dc158c4d8083bdc4b43e178cf0977a2c5eefbcbedcc4e351b0a066a747a38c6c266b9dc2136523cef04395918de37773db63d574aabde59c12eb",
			false,
			2,
			22514,
			"0000000000000000000000000000000000000000",
			"000000000000000000000000000000000000000000000000000000000000002a",
			"f85af8589499466ed2e37b892a2ee3e9cd55a98b68f5735db2e1a024abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503daa0000000000000000000000000000000000000000000000000000000000000002a",
		)

		err := store.StoreTransaction(context.Background(), event)
		require.NoError(t, err)

		resp := rpcRequest(url, request, "origin", "test.com")
		defer resp.Body.Close()

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		assert.Equal(t, expectedResponse, strings.TrimSuffix(string(content), "\n"))
	})

	t.Run("eth_getBalance", func(t *testing.T) {
		request := `{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["0x407d73d8a49eeb85d32cf465507dd71d507100c1","latest"]}`
		expectedResponse := `{"jsonrpc":"2.0","id":1,"result":"0x22ecb25c00"}`

		result, err := cadence.NewUFix64("1500.0")
		require.NoError(t, err)
		mockFlowClient.On(
			"ExecuteScriptAtLatestBlock",
			mock.Anything,
			api.EVMAddressBalance,
			mock.Anything,
		).Once().Return(result, nil)

		resp := rpcRequest(url, request, "origin", "test.com")
		defer resp.Body.Close()

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		assert.Equal(t, expectedResponse, strings.TrimSuffix(string(content), "\n"))
	})
}

func TestServerJSONRPCOveWebSocketHandler(t *testing.T) {
	store := storage.NewStore()
	srv := api.NewHTTPServer(zerolog.Logger{}, rpc.DefaultHTTPTimeouts)
	config := &api.Config{
		ChainID:  api.FlowEVMTestnetChainID,
		Coinbase: common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"),
		GasPrice: api.DefaultGasPrice,
	}
	flowClient, err := api.NewFlowClient(grpc.EmulatorHost)
	require.NoError(t, err)
	blockchainAPI := api.NewBlockChainAPI(config, store, flowClient)
	supportedAPIs := api.SupportedAPIs(blockchainAPI)
	srv.EnableWS(supportedAPIs)
	srv.SetListenAddr("localhost", 8545)
	err = srv.Start()
	defer srv.Stop()
	if err != nil {
		panic(err)
	}

	url := "ws://" + srv.ListenAddr() + "/ws"

	extraHeaders := []string{"Origin", "*"}
	headers := make(http.Header)
	for i := 0; i < len(extraHeaders); i += 2 {
		key, value := extraHeaders[i], extraHeaders[i+1]
		headers.Set(key, value)
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		conn.Close()
		panic(err)
	}
	defer conn.Close()

	done := make(chan struct{})

	expectedResponses := strings.Split(responses, "\n")
	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			assert.Contains(
				t,
				expectedResponses,
				strings.TrimSuffix(string(message), "\n"),
			)
		}
	}()

	for _, request := range strings.Split(requests, "\n") {
		err := conn.WriteMessage(websocket.TextMessage, []byte(request))
		assert.NoError(t, err)
	}
}

// rpcRequest performs a JSON-RPC request to the given URL.
func rpcRequest(url, bodyStr string, extraHeaders ...string) *http.Response {
	// Create the request.
	body := bytes.NewReader([]byte(bodyStr))
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		panic(err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept-encoding", "identity")

	// Apply extra headers.
	if len(extraHeaders)%2 != 0 {
		panic("odd extraHeaders length")
	}
	for i := 0; i < len(extraHeaders); i += 2 {
		key, value := extraHeaders[i], extraHeaders[i+1]
		if strings.EqualFold(key, "host") {
			req.Host = value
		} else {
			req.Header.Set(key, value)
		}
	}

	// Perform the request.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	return resp
}
