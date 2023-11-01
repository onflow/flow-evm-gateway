package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/onflow/flow-go-sdk/access/grpc"
)

type BlockChainAPI struct {
	flowClient *grpc.Client
}

func (api *BlockChainAPI) ChainId() (string, error) {
	ctx := context.Background()
	networkParameters, err := api.flowClient.GetNetworkParameters(ctx)
	return networkParameters.ChainID.String(), err
}

func (api *BlockChainAPI) BlockNumber() (uint64, error) {
	ctx := context.Background()
	latestBlock, err := api.flowClient.GetLatestBlock(ctx, true)
	return latestBlock.Height, err
}

func (api *BlockChainAPI) Syncing() (bool, error) {
	return false, nil
}

func main() {
	server := rpc.NewServer()
	defer server.Stop()

	// You can also use grpc.TestnetHost and grpc.EmulatorHost
	flowClient, _ := grpc.NewClient(grpc.MainnetHost)

	ethAPI := &BlockChainAPI{flowClient: flowClient}
	server.RegisterName("eth", ethAPI)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("can't listen:", err)
	}
	defer listener.Close()
	go server.ServeListener(listener)

	requests := []string{
		`{"jsonrpc":"2.0","id":0,"method":"eth_blockNumber","params": []}`,
		`{"jsonrpc":"2.0","id":0,"method":"eth_chainId","params": []}`,
		`{"jsonrpc":"2.0","id":0,"method":"eth_syncing","params": []}`,
	}
	deadline := time.Now().Add(10 * time.Second)

	for _, request := range requests {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			fmt.Println("can't dial:", err)
		}

		conn.SetDeadline(deadline)
		// Write the request, then half-close the connection so the server stops reading.
		conn.Write([]byte(request))
		conn.(*net.TCPConn).CloseWrite()
		// Now try to get the response.
		buf := make([]byte, 2000)
		n, err := conn.Read(buf)
		conn.Close()

		if err != nil {
			fmt.Println("read error:", err)
		}
		fmt.Println("Request: ", request)
		fmt.Println("Response: ", string(buf[:n]))
	}

}
