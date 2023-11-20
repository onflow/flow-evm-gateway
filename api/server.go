package api

import "github.com/ethereum/go-ethereum/rpc"

const ethNamespace = "eth"

func NewRPCServer() *rpc.Server {
	server := rpc.NewServer()
	server.RegisterName(ethNamespace, &BlockChainAPI{})

	return server
}
