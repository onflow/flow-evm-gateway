package api

import (
	"bytes"
	"crypto/ecdsa"
	"io"
	"math/big"

	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethCrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/flow-go/fvm/evm/emulator"
)

type eoaTestAccount struct {
	address gethCommon.Address
	key     *ecdsa.PrivateKey
	signer  types.Signer
}

func (a *eoaTestAccount) PrepareSignAndEncodeTx(
	to *gethCommon.Address,
	data []byte,
	amount *big.Int,
	gasLimit uint64,
	gasPrice *big.Int,
) []byte {
	tx := a.prepareAndSignTx(to, data, amount, gasLimit, gasPrice)

	var b bytes.Buffer
	writer := io.Writer(&b)
	err := tx.EncodeRLP(writer)
	if err != nil {
		panic(err)
	}

	return b.Bytes()
}

func (a *eoaTestAccount) prepareAndSignTx(
	to *gethCommon.Address,
	data []byte,
	amount *big.Int,
	gasLimit uint64,
	gasPrice *big.Int,
) *types.Transaction {
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
	tx, err := types.SignTx(tx, a.signer, a.key)
	if err != nil {
		panic(err)
	}

	return tx
}

func newEOATestAccount(keyHex string) *eoaTestAccount {
	key, _ := gethCrypto.HexToECDSA(keyHex)
	address := gethCrypto.PubkeyToAddress(key.PublicKey)
	signer := emulator.GetDefaultSigner()

	return &eoaTestAccount{
		address: address,
		key:     key,
		signer:  signer,
	}
}
