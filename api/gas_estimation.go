package api

import (
	"fmt"
	"math/big"

	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethCrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/flow-go/fvm/evm/emulator"
)

var key, _ = gethCrypto.HexToECDSA("45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8")
var signer = emulator.GetDefaultSigner()

// signGasEstimationTx will create a transaction and sign it with a test account
// used only for gas estimation, since gas estimation is run inside a script
// and does not change the state this account is not important it just has to be valid.
func signGasEstimationTx(
	to *gethCommon.Address,
	data []byte,
	amount *big.Int,
	gasLimit uint64,
	gasPrice *big.Int,
) ([]byte, error) {
	tx := types.NewTx(
		&types.LegacyTx{
			Nonce:    0,
			To:       to,
			Value:    amount,
			Gas:      gasLimit,
			GasPrice: gasPrice,
			Data:     data,
		},
	)
	tx, err := types.SignTx(tx, signer, key)
	if err != nil {
		return nil, fmt.Errorf("failed to sign tx for gas estimation: %w", err)
	}

	return tx.MarshalBinary()
}
