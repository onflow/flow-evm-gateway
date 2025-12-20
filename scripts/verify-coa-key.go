//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"os"

	"github.com/onflow/flow-go-sdk/crypto"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run verify-coa-key.go <private-key-hex>")
		fmt.Println("Example: go run verify-coa-key.go cce21bbab15306774c4cb71ce84fb0a6294dc5121acf70c36f51a3b26362ce38")
		os.Exit(1)
	}

	privateKeyHex := os.Args[1]

	// Decode private key (ECDSA_secp256k1 based on account keys)
	privateKey, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_secp256k1, privateKeyHex)
	if err != nil {
		fmt.Printf("Error decoding private key: %v\n", err)
		fmt.Println("\nTrying ECDSA_P256...")
		privateKey, err = crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, privateKeyHex)
		if err != nil {
			fmt.Printf("Error decoding private key as ECDSA_P256: %v\n", err)
			os.Exit(1)
		}
	}

	// Get public key
	publicKey := privateKey.PublicKey()

	// Print the public key
	derivedPubKey := publicKey.String()
	expectedPubKey := "ce460cc97720488f2a6313962520e6455d4d0d3c29490a556ab6bfd39102ba39d9f483a9d4e9bc5f09129114a1ab2c079d63e3c090f135a29ec5414d1bcd3634"

	// Remove 0x prefix if present for comparison
	derivedPubKeyNoPrefix := derivedPubKey
	if len(derivedPubKey) > 2 && derivedPubKey[:2] == "0x" {
		derivedPubKeyNoPrefix = derivedPubKey[2:]
	}

	fmt.Printf("Public key derived from private key:\n")
	fmt.Printf("%s\n", derivedPubKey)
	fmt.Printf("\nExpected public key (from key index 0):\n")
	fmt.Printf("%s\n", expectedPubKey)

	match := derivedPubKeyNoPrefix == expectedPubKey
	fmt.Printf("\nMatch: %v\n", match)

	if match {
		fmt.Printf("\n✅ SUCCESS: Keys match! The gateway should load all 101 keys.\n")
		fmt.Printf("If you're still seeing 'no signing keys available', check:\n")
		fmt.Printf("1. Gateway was restarted after adding keys\n")
		fmt.Printf("2. No errors in startup logs\n")
		fmt.Printf("3. Keys are not all locked/in use\n")
	} else {
		fmt.Printf("\n❌ MISMATCH: Keys don't match!\n")
		fmt.Printf("The public key from COA_KEY doesn't match key index 0's public key.\n")
		fmt.Printf("You need to either:\n")
		fmt.Printf("1. Update COA_KEY to use the private key for key index 0, OR\n")
		fmt.Printf("2. Add new keys using the public key that matches your current COA_KEY\n")
	}
}
