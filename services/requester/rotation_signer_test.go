package requester

import (
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_KeyRotation(t *testing.T) {

	t.Run("test keys being rotated correctly in non-concurrent access", func(t *testing.T) {
		keys := testKeys(t)
		krs, err := NewKeyRotationSigner(keys, crypto.SHA3_256)
		require.NoError(t, err)

		data := []byte("foo")
		// make sure we make enough iterations and are odd
		for i := 0; i < len(keys)*3+3; i++ {
			sig, err := krs.Sign(data)
			require.NoError(t, err)

			// make sure we used the correct key to sign and it matches pub key
			index := i % len(keys)
			pk := keys[index]
			assert.True(t, pk.PublicKey().Equals(krs.PublicKey()))

			valid, err := krs.PublicKey().Verify(sig, data, krs.hasher)
			require.NoError(t, err)
			assert.True(t, valid)
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
