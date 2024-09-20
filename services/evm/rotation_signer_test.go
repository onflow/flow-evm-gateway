package evm

import (
	"fmt"
	"sync"
	"testing"

	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"
)

func Test_KeyRotation(t *testing.T) {

	t.Run("keys being rotated correctly in non-concurrent access", func(t *testing.T) {
		keys := testKeys(t)
		krs, err := NewKeyRotationSigner(keys, crypto.SHA3_256)
		require.NoError(t, err)

		data := []byte("foo")
		// make sure we make enough iterations and are odd
		for i := 0; i < len(keys)*3+3; i++ {
			// make sure we used the correct key to sign and it matches pub key
			index := i % len(keys)
			pk := keys[index]
			krsPubKey := krs.PublicKey()

			assert.True(t, pk.PublicKey().Equals(krsPubKey), fmt.Sprintf(
				"pub key not correct, should be: %s, but is: %s",
				pk.PublicKey().String(),
				krs.PublicKey().String(),
			))

			sig, err := krs.Sign(data)
			require.NoError(t, err)

			valid, err := krsPubKey.Verify(sig, data, krs.hasher)
			require.NoError(t, err)
			assert.True(t, valid, "signature not valid for key")
		}
	})

	// this test makes sure that if we concurrently sign we use each key the same times
	t.Run("keys being correctly rotate in concurrent access", func(t *testing.T) {
		keys := testKeys(t)
		krs, err := NewKeyRotationSigner(keys, crypto.SHA3_256)
		require.NoError(t, err)

		data := []byte("bar")
		keyIterations := 20                     // 10 times each key
		iterations := len(keys) * keyIterations // total iterations
		sigs := make(chan []byte, iterations)

		wg := sync.WaitGroup{}
		wg.Add(iterations)

		for i := 0; i < iterations; i++ {
			go func() {
				sig, err := krs.Sign(data)
				sigs <- sig
				require.NoError(t, err)
				wg.Done()
			}()
		}

		wg.Wait()
		close(sigs)
		counters := make(map[int]int)
		// check if each key was used same times
		for sig := range sigs {
			// validate signature to check which key was used
			for keyIndex, pk := range krs.keys {
				ok, _ := pk.PublicKey().Verify(sig, data, krs.hasher)
				if ok {
					counters[keyIndex]++
				}
			}
		}

		// make sure we have correct number of keys used
		assert.Len(t, maps.Keys(counters), len(keys))

		for _, c := range counters {
			assert.Equal(t, keyIterations, c)
		}
	})

}

func testKeys(t *testing.T) []crypto.PrivateKey {
	pkeys := []string{
		"e82b6b746bbd9cd687fae8d0c7c5a2f32fe0496839a908aa80f840b27344a13b",
		"2504246efc55f863c3278e4e6f0bbb6d2c5484c3f7669a6c9ac45ee072a21fc0",
		"7e20cf9383853f850f342ada91071661df00ec0be28afd4a4bc502d101e2fbed",
		"2a06a8cf79b7f7cc2e56cb55de677920cc4e0d70404bdfe6a15c64625459ca80",
		"311ed662abd573ed1f7109f1dd2e16755cf922fe0780d8ce2d36ce62f4301965",
	}
	keys := make([]crypto.PrivateKey, len(pkeys))
	for i, pk := range pkeys {
		k, err := crypto.DecodePrivateKeyHex(crypto.ECDSA_P256, pk)
		require.NoError(t, err)
		keys[i] = k
	}

	return keys
}
