package api_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

var requests = []string{
	`{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params": []}`,
	`{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber","params": []}`,
	`{"jsonrpc":"2.0","id":3,"method":"eth_syncing","params": []}`,
	`{"jsonrpc":"2.0","id":4,"method":"eth_sendRawTransaction","params":["0xd46e8dd67c5d32be8d46e8dd67c5d32be8058bb8eb970870f072445675058bb8eb970870f072445675"]}`,
	`{"jsonrpc":"2.0","id":5,"method":"eth_gasPrice","params":[]}`,
	`{"jsonrpc":"2.0","id":6,"method":"eth_getBalance","params":["0x407d73d8a49eeb85d32cf465507dd71d507100c1","latest"]}`,
	`{"jsonrpc":"2.0","id":7,"method":"eth_getCode","params":["0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b","0x2"]}`,
	`{"jsonrpc":"2.0","id":8,"method":"eth_getStorageAt","params":["0x295a70b2de5e3953354a6a8344e616ed314d7251","0x6661e9d6d8b923d5bbaab1b96e1dd51ff6ea2a93520fdc9eb75d059238b8c5e9","latest"]}`,
	`{"jsonrpc":"2.0","id":9,"method":"eth_getTransactionCount","params":["0x407d73d8a49eeb85d32cf465507dd71d507100c1","latest"]}`,
	`{"jsonrpc":"2.0","id":10,"method":"eth_getTransactionByHash","params":["0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b"]}`,
	`{"jsonrpc":"2.0","id":11,"method":"eth_getTransactionByBlockHashAndIndex","params":["0xc6ef2fc5426d6ad6fd9e2a26abeab0aa2411b7ab17f30a99d3cb96aed1d1055b", "0x0"]}`,
	`{"jsonrpc":"2.0","id":12,"method":"eth_getTransactionByBlockNumberAndIndex","params":["0x29c", "0x0"]}`,
	`{"jsonrpc":"2.0","id":13,"method":"eth_getTransactionReceipt","params":["0x85d995eba9763907fdf35cd2034144dd9d53ce32cbec21349d4b12823c6860c5"]}`,
	`{"jsonrpc":"2.0","id":14,"method":"eth_coinbase"}`,
	`{"jsonrpc":"2.0","id":15,"method":"eth_getBlockByHash","params":["0xdc0818cf78f21a8e70579cb46a43643f78291264dda342ae31049421c82d21ae",false]}`,
	`{"jsonrpc":"2.0","id":16,"method":"eth_getBlockByNumber","params":["0x1b4",true]}`,
	`{"jsonrpc":"2.0","id":17,"method":"eth_getBlockTransactionCountByHash","params":["0xb903239f8543d04b5dc1ba6579132b143087c68db1b2168786408fcbce568238"]}`,
	`{"jsonrpc":"2.0","id":18,"method":"eth_getBlockTransactionCountByNumber","params":["0xe8"]}`,
	`{"jsonrpc":"2.0","id":19,"method":"eth_getUncleCountByBlockHash","params":["0xb903239f8543d04b5dc1ba6579132b143087c68db1b2168786408fcbce568238"]}`,
	`{"jsonrpc":"2.0","id":20,"method":"eth_getUncleCountByBlockNumber","params":["0xe8"]}`,
	`{"jsonrpc":"2.0","id":21,"method":"eth_getLogs","params":[{"topics":["0x000000000000000000000000a94f5374fce5edbc8e2a8697c15331677e6ebf0b"]}]}`,
	`{"jsonrpc":"2.0","id":22,"method":"eth_newFilter","params":[{"topics":["0x000000000000000000000000a94f5374fce5edbc8e2a8697c15331677e6ebf0b"]}]}`,
	`{"jsonrpc":"2.0","id":23,"method":"eth_uninstallFilter","params":["0xb"]}`,
	`{"jsonrpc":"2.0","id":24,"method":"eth_getFilterLogs","params":["0x16"]}`,
	`{"jsonrpc":"2.0","id":25,"method":"eth_getFilterChanges","params":["0x16"]}`,
	`{"jsonrpc":"2.0","id":26,"method":"eth_newBlockFilter","params":[]}`,
	`{"jsonrpc":"2.0","id":27,"method":"eth_newPendingTransactionFilter","params":[]}`,
	`{"jsonrpc":"2.0","id":28,"method":"eth_accounts","params":[]}`,
	`{"jsonrpc":"2.0","id":29,"method":"eth_call","params":[{"from":"0xb60e8dd61c5d32be8058bb8eb970870f07233155","to":"0xd46e8dd67c5d32be8058bb8eb970870f07244567","gas":"0x76c0","gasPrice":"0x9184e72a000","value":"0x9184e72a","input":"0xd46e8dd67c5d32be8d46e8dd67c5d32be8058bb8eb970870f072445675058bb8eb970870f072445675"}]}`,
	`{"jsonrpc":"2.0","id":30,"method":"eth_estimateGas","params":[{"from":"0xb60e8dd61c5d32be8058bb8eb970870f07233155","to":"0xd46e8dd67c5d32be8058bb8eb970870f07244567","gas":"0x76c0","gasPrice":"0x9184e72a000","value":"0x9184e72a","input":"0xd46e8dd67c5d32be8d46e8dd67c5d32be8058bb8eb970870f072445675058bb8eb970870f072445675"}]}`,
	`{"jsonrpc":"2.0","id":31,"method":"eth_getUncleByBlockHashAndIndex","params":["0xc6ef2fc5426d6ad6fd9e2a26abeab0aa2411b7ab17f30a99d3cb96aed1d1055b", "0x45"]}`,
	`{"jsonrpc":"2.0","id":32,"method":"eth_getUncleByBlockNumberAndIndex","params":["0xe8", "0x45"]}`,
}

var expectedResponses = []string{
	`{"jsonrpc":"2.0","id":1,"result":"0x29a"}`,
	`{"jsonrpc":"2.0","id":2,"result":"0x0"}`,
	`{"jsonrpc":"2.0","id":3,"result":false}`,
	`{"jsonrpc":"2.0","id":4,"result":"0x47173285a8d7341e5e972fc677286384f802f8ef42a5ec5f03bbfa254cb01fad"}`,
	`{"jsonrpc":"2.0","id":5,"result":"0x1dfd14000"}`,
	`{"jsonrpc":"2.0","id":6,"result":"0x65"}`,
	`{"jsonrpc":"2.0","id":7,"result":"0x600160008035811a818181146012578301005b601b6001356025565b8060005260206000f25b600060078202905091905056"}`,
	`{"jsonrpc":"2.0","id":8,"result":"0x600160008035811a818181146012578301005b601b6001356025565b8060005260206000f25b600060078202905091905056"}`,
	`{"jsonrpc":"2.0","id":9,"result":"0x10078e"}`,
	`{"jsonrpc":"2.0","id":10,"result":{"blockHash":"0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2","blockNumber":"0x5daf3b","from":"0xa7d9ddbe1f17865597fbd27ec712455208b6b76d","gas":"0xc350","gasPrice":"0x4a817c800","hash":"0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b","input":"0x3078363836353663366336663231","nonce":"0x15","to":"0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb","transactionIndex":"0x40","value":"0xf3dbb76162000","type":"0x0","v":"0x25","r":"0x96","s":"0xfa"}}`,
	`{"jsonrpc":"2.0","id":11,"result":{"blockHash":"0xc6ef2fc5426d6ad6fd9e2a26abeab0aa2411b7ab17f30a99d3cb96aed1d1055b","blockNumber":"0x5daf3b","from":"0xa7d9ddbe1f17865597fbd27ec712455208b6b76d","gas":"0xc350","gasPrice":"0x4a817c800","hash":"0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b","input":"0x3078363836353663366336663231","nonce":"0x15","to":"0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb","transactionIndex":"0x40","value":"0xf3dbb76162000","type":"0x0","v":"0x25","r":"0x96","s":"0xfa"}}`,
	`{"jsonrpc":"2.0","id":12,"result":{"blockHash":"0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2","blockNumber":"0x5daf3b","from":"0xa7d9ddbe1f17865597fbd27ec712455208b6b76d","gas":"0xc350","gasPrice":"0x4a817c800","hash":"0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b","input":"0x3078363836353663366336663231","nonce":"0x15","to":"0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb","transactionIndex":"0x40","value":"0xf3dbb76162000","type":"0x0","v":"0x25","r":"0x96","s":"0xfa"}}`,
	`{"jsonrpc":"2.0","id":13,"result":{"blockHash":"0x1d59ff54b1eb26b013ce3cb5fc9dab3705b415a67127a003c3e61eb445bb8df2","blockNumber":"0x5daf3b","contractAddress":null,"cumulativeGasUsed":"0xc350","effectiveGasPrice":"0x4a817c800","from":"0xa7d9ddbe1f17865597fbd27ec712455208b6b76d","gasUsed":"0x9c40","logs":[],"logsBloom":"0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b","status":"0x1","to":"0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb","transactionHash":"0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b","transactionIndex":"0x40","type":"0x2"}}`,
	`{"jsonrpc":"2.0","id":14,"result":"0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"}`,
	`{"jsonrpc":"2.0","id":15,"result":{"difficulty":"0x4ea3f27bc","extraData":"0x476574682f4c5649562f76312e302e302f6c696e75782f676f312e342e32","gasLimit":"0x1388","gasUsed":"0x0","hash":"0xdc0818cf78f21a8e70579cb46a43643f78291264dda342ae31049421c82d21ae","logsBloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","miner":"0xbb7b8287f3f0a933474a79eae42cbca977791171","mixHash":"0x4fffe9ae21f1c9e15207b1f472d5bbdd68c9595d461666602f2be20daf5e7843","nonce":"0x689056015818adbe","number":"0x1b4","parentHash":"0xe99e022112df268087ea7eafaf4790497fd21dbeeb6bd7a1721df161a6657a54","receiptsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","sha3Uncles":"0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347","size":"0x220","stateRoot":"0xddc8b0234c2e0cad087c8b389aa7ef01f7d79b2570bccb77ce48648aa61c904d","timestamp":"0x55ba467c","totalDifficulty":"0x78ed983323d","transactions":[],"transactionsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","uncles":[]}}`,
	`{"jsonrpc":"2.0","id":16,"result":{"difficulty":"0x4ea3f27bc","extraData":"0x476574682f4c5649562f76312e302e302f6c696e75782f676f312e342e32","gasLimit":"0x1388","gasUsed":"0x0","hash":"0xdc0818cf78f21a8e70579cb46a43643f78291264dda342ae31049421c82d21ae","logsBloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","miner":"0xbb7b8287f3f0a933474a79eae42cbca977791171","mixHash":"0x4fffe9ae21f1c9e15207b1f472d5bbdd68c9595d461666602f2be20daf5e7843","nonce":"0x689056015818adbe","number":"0x1b4","parentHash":"0xe99e022112df268087ea7eafaf4790497fd21dbeeb6bd7a1721df161a6657a54","receiptsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","sha3Uncles":"0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347","size":"0x220","stateRoot":"0xddc8b0234c2e0cad087c8b389aa7ef01f7d79b2570bccb77ce48648aa61c904d","timestamp":"0x55ba467c","totalDifficulty":"0x78ed983323d","transactions":[],"transactionsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","uncles":[]}}`,
	`{"jsonrpc":"2.0","id":17,"result":"0x188aa"}`,
	`{"jsonrpc":"2.0","id":18,"result":"0x20a"}`,
	`{"jsonrpc":"2.0","id":19,"result":"0x0"}`,
	`{"jsonrpc":"2.0","id":20,"result":"0x0"}`,
	`{"jsonrpc":"2.0","id":21,"result":[{"address":"0x16c5785ac562ff41e2dcfdf829c5a142f1fccd7d","topics":["0x59ebeb90bc63057b6515673c3ecf9438e5058bca0f92585014eced636878c9a5"],"data":"0x000000","blockNumber":"0x1b4","transactionHash":"0x00df829c5a142f1fccd7d8216c5785ac562ff41e2dcfdf5785ac562ff41e2dcf","transactionIndex":"0x0","blockHash":"0x008216c5785ac562ff41e2dcfdf5785ac562ff41e2dcfdf829c5a142f1fccd7d","logIndex":"0x1","removed":false}]}`,
	`{"jsonrpc":"2.0","id":22,"result":"filter0"}`,
	`{"jsonrpc":"2.0","id":23,"result":true}`,
	`{"jsonrpc":"2.0","id":24,"result":[{"address":"0x16c5785ac562ff41e2dcfdf829c5a142f1fccd7d","topics":["0x59ebeb90bc63057b6515673c3ecf9438e5058bca0f92585014eced636878c9a5"],"data":"0x000000","blockNumber":"0x1b4","transactionHash":"0x00df829c5a142f1fccd7d8216c5785ac562ff41e2dcfdf5785ac562ff41e2dcf","transactionIndex":"0x0","blockHash":"0x008216c5785ac562ff41e2dcfdf5785ac562ff41e2dcfdf829c5a142f1fccd7d","logIndex":"0x1","removed":false}]}`,
	`{"jsonrpc":"2.0","id":25,"result":[{"address":"0x16c5785ac562ff41e2dcfdf829c5a142f1fccd7d","topics":["0x59ebeb90bc63057b6515673c3ecf9438e5058bca0f92585014eced636878c9a5"],"data":"0x000000","blockNumber":"0x1b4","transactionHash":"0x00df829c5a142f1fccd7d8216c5785ac562ff41e2dcfdf5785ac562ff41e2dcf","transactionIndex":"0x0","blockHash":"0x008216c5785ac562ff41e2dcfdf5785ac562ff41e2dcfdf829c5a142f1fccd7d","logIndex":"0x1","removed":false}]}`,
	`{"jsonrpc":"2.0","id":26,"result":"block_filter"}`,
	`{"jsonrpc":"2.0","id":27,"result":"pending_tx_filter"}`,
	`{"jsonrpc":"2.0","id":28,"result":["0x407d73d8a49eeb85d32cf465507dd71d507100c1"]}`,
	`{"jsonrpc":"2.0","id":29,"result":"0x00010203040506070809"}`,
	`{"jsonrpc":"2.0","id":30,"result":"0x69"}`,
	`{"jsonrpc":"2.0","id":31,"result":{}}`,
	`{"jsonrpc":"2.0","id":32,"result":{}}`,
}

func TestServerJSONRPCOveHTTPHandler(t *testing.T) {
	store := storage.NewStore()
	srv := api.NewHTTPServer(zerolog.Logger{}, rpc.DefaultHTTPTimeouts)
	config := &api.Config{
		ChainID:  api.FlowEVMTestnetChainID,
		Coinbase: common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"),
	}
	supportedAPIs := api.SupportedAPIs(config, store)
	srv.EnableRPC(supportedAPIs)
	srv.SetListenAddr("localhost", 8545)
	err := srv.Start()
	defer srv.Stop()
	if err != nil {
		panic(err)
	}

	url := "http://" + srv.ListenAddr() + "/rpc"

	for i, request := range requests {
		resp := rpcRequest(url, request, "origin", "test.com")
		defer resp.Body.Close()
		content, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		expectedResponse := expectedResponses[i]

		assert.Equal(t, expectedResponse, strings.TrimSuffix(string(content), "\n"))
	}
}

func TestServerJSONRPCOveWebSocketHandler(t *testing.T) {
	store := storage.NewStore()
	srv := api.NewHTTPServer(zerolog.Logger{}, rpc.DefaultHTTPTimeouts)
	config := &api.Config{
		ChainID:  api.FlowEVMTestnetChainID,
		Coinbase: common.HexToAddress("0xf02c1c8e6114b1dbe8937a39260b5b0a374432bb"),
	}
	supportedAPIs := api.SupportedAPIs(config, store)
	srv.EnableWS(supportedAPIs)
	srv.SetListenAddr("localhost", 8545)
	err := srv.Start()
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

	for _, request := range requests {
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
