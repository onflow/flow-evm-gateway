package api_test

import (
	"bytes"
	_ "embed"
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

//go:embed fixtures/eth_json_rpc_requests.json
var requests string

//go:embed fixtures/eth_json_rpc_responses.json
var responses string

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
