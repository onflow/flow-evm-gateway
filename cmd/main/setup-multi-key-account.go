package main

import (
	"flag"
	"fmt"

	"github.com/onflow/flow-evm-gateway/bootstrap"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/access/grpc"
	"github.com/onflow/flow-go-sdk/crypto"
)

/*
*
This command creates a new account with multiple keys, which are saved to keys.json for later
use with running the gateway in a key-rotation mode (used with --coa-key-file flag).
*/
func SetupKey() {
	var (
		keyCount                                         int
		keyFlag, addressFlag, hostFlag, ftFlag, flowFlag string
	)

	flag.IntVar(&keyCount, "key-count", 20, "how many keys you want to create and assign to account")
	flag.StringVar(&keyFlag, "signer-key", "", "signer key used to create the new account")
	flag.StringVar(&addressFlag, "signer-address", "", "signer address used to create new account")
	flag.StringVar(&ftFlag, "ft-address", "0xee82856bf20e2aa6", "address of fungible token contract")
	flag.StringVar(&flowFlag, "flow-token-address", "0x0ae53cb6e3f42a79", "address of flow token contract")
	flag.StringVar(&hostFlag, "access-node-grpc-host", "localhost:3569", "host to the flow access node gRPC API")

	flag.Parse()

	key, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, keyFlag)
	if err != nil {
		panic(key)
	}

	payer := flow.HexToAddress(addressFlag)
	if payer == flow.EmptyAddress {
		panic("invalid address")
	}

	client, err := grpc.NewClient(hostFlag)
	if err != nil {
		panic(err)
	}

	address, keys, err := bootstrap.CreateMultiKeyAccount(client, keyCount, payer, flowFlag, ftFlag, key)
	if err != nil {
		panic(err)
	}

	fmt.Println("Address: ", address.Hex())
	fmt.Println("Keys:")
	for _, pk := range keys {
		fmt.Println(pk.String())
	}
}
