package api

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/flow-go/fvm/evm/emulator"
)

const defaultGasLimit uint64 = 15_000_000

var key, _ = crypto.HexToECDSA("45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8")
var signer = emulator.GetDefaultSigner()

// signTxFromArgs will create a transaction from the given arguments and sign it
// with a test account. The resulting signed transaction is only supposed to be
// used through `EVM.run` inside Cadence scripts, meaning that no state change
// will occur. This is only useful for `eth_estimateGas` and `eth_call` endpoints.
func signTxFromArgs(args TransactionArgs) ([]byte, error) {
	var data []byte
	if args.Data != nil {
		data = *args.Data
	} else if args.Input != nil {
		data = *args.Input
	}

	// provide a high enough gas for the tx to be able to execute,
	// capped by the gas set in transaction args.
	gasLimit := defaultGasLimit
	if args.Gas != nil {
		gasLimit = uint64(*args.Gas)
	}

	value := big.NewInt(0)
	if args.Value != nil {
		value = args.Value.ToInt()
	}

	tx := types.NewTx(
		&types.LegacyTx{
			Nonce:    0,
			To:       args.To,
			Value:    value,
			Gas:      gasLimit,
			GasPrice: big.NewInt(0),
			Data:     data,
		},
	)

	tx, err := types.SignTx(tx, signer, key)
	if err != nil {
		return nil, fmt.Errorf("failed to sign tx from args: %w", err)
	}

	return tx.MarshalBinary()
}
