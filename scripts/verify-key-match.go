//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"os"

	"github.com/onflow/flow-go-sdk/crypto"
)

func main() {
	// Your values
	privateKeyHex := "cce21bbab15306774c4cb71ce84fb0a6294dc5121acf70c36f51a3b26362ce38"
	expectedPublicKeyHex := "ce460cc97720488f2a6313962520e6455d4d0d3c29490a556ab6bfd39102ba39d9f483a9d4e9bc5f09129114a1ab2c079d63e3c090f135a29ec5414d1bcd3634"

	fmt.Println("=== COA Key Verification ===")
	fmt.Println()

	// Decode private key as ECDSA_secp256k1 (based on account keys)
	privateKey, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_secp256k1, privateKeyHex)
	if err != nil {
		fmt.Printf("❌ Error decoding private key as ECDSA_secp256k1: %v\n", err)
		fmt.Println("\nTrying ECDSA_P256...")
		privateKey, err = crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, privateKeyHex)
		if err != nil {
			fmt.Printf("❌ Error decoding private key as ECDSA_P256: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Decoded as ECDSA_P256")
	} else {
		fmt.Println("✅ Decoded as ECDSA_secp256k1")
	}

	// Get public key
	publicKey := privateKey.PublicKey()
	derivedPublicKeyHex := publicKey.String()

	// Remove 0x prefix if present
	if len(derivedPublicKeyHex) > 2 && derivedPublicKeyHex[:2] == "0x" {
		derivedPublicKeyHex = derivedPublicKeyHex[2:]
	}

	fmt.Println()
	fmt.Println("=== Comparison ===")
	fmt.Printf("Derived Public Key: %s\n", derivedPublicKeyHex)
	fmt.Printf("Expected (Key 0):   %s\n", expectedPublicKeyHex)
	fmt.Println()

	match := derivedPublicKeyHex == expectedPublicKeyHex
	if match {
		fmt.Println("✅ MATCH: Keys are identical!")
		fmt.Println()
		fmt.Println("This means:")
		fmt.Println("  • Your COA_KEY corresponds to key index 0")
		fmt.Println("  • All 101 keys on the account should be loaded")
		fmt.Println("  • If you're still seeing 'no signing keys available',")
		fmt.Println("    the issue is likely:")
		fmt.Println("    1. Gateway wasn't restarted after adding keys")
		fmt.Println("    2. All keys are currently locked/in use")
		fmt.Println("    3. There's a bug in key loading logic")
	} else {
		fmt.Println("❌ MISMATCH: Keys don't match!")
		fmt.Println()
		fmt.Println("This means:")
		fmt.Println("  • Your COA_KEY does NOT correspond to key index 0")
		fmt.Println("  • The gateway will NOT load any keys")
		fmt.Println("  • You need to either:")
		fmt.Println("    1. Update COA_KEY to use the private key for key index 0, OR")
		fmt.Println("    2. Add new keys to the account using the public key")
		fmt.Println("       that matches your current COA_KEY")
	}

	fmt.Println()
	fmt.Println("=== Additional Verification ===")
	fmt.Println("You can also verify by checking the account:")
	fmt.Println("  flow accounts get 0xdd4a4464762431db --network testnet")
	fmt.Println()
	fmt.Println("And comparing key index 0's public key with the derived key above.")
}
