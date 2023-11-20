package main

import (
	"fmt"
	"net"
	"time"

	"github.com/onflow/flow-evm-gateway/api"
)

func main() {
	server := api.NewRPCServer()
	defer server.Stop()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("can't listen:", err)
	}
	defer listener.Close()
	go server.ServeListener(listener)

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params": []}`,
		`{"jsonrpc":"2.0","id":2,"method":"eth_chainId","params": []}`,
		`{"jsonrpc":"2.0","id":3,"method":"eth_syncing","params": []}`,
		`{"jsonrpc":"2.0","id":4,"method":"eth_getBalance","params": ["0x407d73d8a49eeb85d32cf465507dd71d507100c1"]}`,
		`{"jsonrpc":"2.0","id":5,"method":"eth_getBlockTransactionCountByNumber","params": ["0x4E4ee"]}`,
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
