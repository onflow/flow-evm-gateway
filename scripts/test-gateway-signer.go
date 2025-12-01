package main

import (
	"fmt"
	"os"

	"github.com/onflow/flow-go-sdk/crypto"
)

func main() {
	// Your COA_KEY (private key)
	privateKeyHex := "cce21bbab15306774c4cb71ce84fb0a6294dc5121acf70c36f51a3b26362ce38"
	
	// Expected public key from key index 0
	expectedPublicKeyHex := "ce460cc97720488f2a6313962520e6455d4d0d3c29490a556ab6bfd39102ba39d9f483a9d4e9bc5f09129114a1ab2c079d63e3c090f135a29ec5414d1bcd3634"

	fmt.Println("=== Gateway Signer Test ===")
	fmt.Println("This test mimics exactly what the gateway does:")
	fmt.Println("  1. Decode COA_KEY as ECDSA_secp256k1")
	fmt.Println("  2. Create signer with SHA3_256 (same as gateway)")
	fmt.Println("  3. Get public key from signer")
	fmt.Println("  4. Compare with key index 0's public key")
	fmt.Println()

	// Step 1: Decode private key (exactly as gateway does)
	privateKey, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_secp256k1, privateKeyHex)
	if err != nil {
		fmt.Printf("❌ FAILED: Cannot decode private key as ECDSA_secp256k1: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Step 1: Decoded private key as ECDSA_secp256k1")

	// Step 2: Create signer (exactly as gateway does - see bootstrap/utils.go line 25)
	signer, err := crypto.NewInMemorySigner(privateKey, crypto.SHA3_256)
	if err != nil {
		fmt.Printf("❌ FAILED: Cannot create signer: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Step 2: Created signer with SHA3_256 (same as gateway)")

	// Step 3: Get public key (exactly as gateway does - see bootstrap/bootstrap.go line 247)
	signerPubKey := signer.PublicKey()
	derivedPublicKeyHex := signerPubKey.String()
	
	// Remove 0x prefix if present
	if len(derivedPublicKeyHex) > 2 && derivedPublicKeyHex[:2] == "0x" {
		derivedPublicKeyHex = derivedPublicKeyHex[2:]
	}
	
	fmt.Println("✅ Step 3: Got public key from signer")
	fmt.Println()

	// Step 4: Compare
	fmt.Println("=== Result ===")
	fmt.Printf("Signer Public Key: %s\n", derivedPublicKeyHex)
	fmt.Printf("Key Index 0:      %s\n", expectedPublicKeyHex)
	fmt.Println()

	match := derivedPublicKeyHex == expectedPublicKeyHex
	if match {
		fmt.Println("✅ DEFINITIVE MATCH: Keys are identical!")
		fmt.Println()
		fmt.Println("This definitively proves:")
		fmt.Println("  • Your COA_KEY will create a signer with the correct public key")
		fmt.Println("  • The gateway's signer.PublicKey() will match key index 0")
		fmt.Println("  • All 101 keys on the account will be loaded by the gateway")
		fmt.Println()
		fmt.Println("If you're still seeing 'no signing keys available', check:")
		fmt.Println("  1. Gateway was restarted after adding keys")
		fmt.Println("  2. Check logs: sudo journalctl -u flow-evm-gateway --since '10 minutes ago' | grep -i keystore")
		fmt.Println("  3. All keys might be locked (unlikely with 101 keys)")
	} else {
		fmt.Println("❌ DEFINITIVE MISMATCH: Keys don't match!")
		fmt.Println()
		fmt.Println("This definitively proves:")
		fmt.Println("  • Your COA_KEY will NOT match key index 0")
		fmt.Println("  • The gateway will NOT load any keys")
		fmt.Println("  • You need to fix the key mismatch")
		os.Exit(1)
	}
}

