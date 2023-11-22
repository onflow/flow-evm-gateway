package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-evm-gateway/api"
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/rs/zerolog"
)

func apis() []rpc.API {
	return []rpc.API{
		{
			Namespace: api.EthNamespace,
			Service: &api.BlockChainAPI{
				Store: storage.NewStore(),
			},
		},
	}
}

func main() {
	timeouts := &rpc.DefaultHTTPTimeouts
	srv := api.NewHTTPServer(zerolog.Logger{}, *timeouts)
	srv.EnableRPC(apis(), api.HttpConfig{})
	// srv.enableWS(nil, *wsConf)
	srv.SetListenAddr("localhost", 8080)
	err := srv.Start()
	if err != nil {
		panic(err)
	}
	fmt.Println("Server Started: ", srv.ListenAddr())

	url := "http://" + srv.ListenAddr()
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params": []}`,
		`{"jsonrpc":"2.0","id":2,"method":"eth_chainId","params": []}`,
		`{"jsonrpc":"2.0","id":3,"method":"eth_syncing","params": []}`,
		`{"jsonrpc":"2.0","id":4,"method":"eth_getBalance","params": ["0x407d73d8a49eeb85d32cf465507dd71d507100c1"]}`,
		`{"jsonrpc":"2.0","id":5,"method":"eth_getBlockTransactionCountByNumber","params": ["0x4E4ee"]}`,
	}
	for _, request := range requests {
		resp := rpcRequest(url, request, "origin", "test.com")
		defer resp.Body.Close()
		content, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		fmt.Println("Response: ", string(content))
	}

	runtime.Goexit()
}

// rpcRequest performs a JSON-RPC request to the given URL.
func rpcRequest(url, bodyStr string, extraHeaders ...string) *http.Response {
	// Create the request.
	body := bytes.NewReader([]byte(bodyStr))
	fmt.Println("Request: ", bodyStr)
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
	fmt.Printf("checking RPC/HTTP on %s %v\n", url, extraHeaders)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	return resp
}
