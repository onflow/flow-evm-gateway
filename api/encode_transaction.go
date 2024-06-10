package api

import (
	"math/big"

	"github.com/onflow/go-ethereum/core/types"
)

const blockGasLimit uint64 = 15_000_000

// encodeTxFromArgs will create a transaction from the given arguments.
// The resulting unsigned transaction is only supposed to be used through
// `EVM.dryRun` inside Cadence scripts, meaning that no state change
// will occur.
// This is only useful for `eth_estimateGas` and `eth_call` endpoints.
func encodeTxFromArgs(args TransactionArgs) ([]byte, error) {
	var data []byte
	if args.Data != nil {
		data = *args.Data
	} else if args.Input != nil {
		data = *args.Input
	}

	// provide a high enough gas for the tx to be able to execute,
	// capped by the gas set in transaction args.
	gasLimit := blockGasLimit
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

	return tx.MarshalBinary()
}
