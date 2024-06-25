package models

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/types"
)

func ValidateTransaction(tx *types.Transaction) error {
	txDataLen := len(tx.Data())

	// Contract creation doesn't validate call data, handle first
	if tx.To() == nil {
		// Contract creation should contain sufficient data to deploy a contract. A
		// typical error is omitting sender due to some quirk in the javascript call
		// e.g. https://github.com/onflow/go-ethereum/issues/16106.
		if txDataLen == 0 {
			// Prevent sending ether into black hole (show stopper)
			if tx.Value().Cmp(big.NewInt(0)) > 0 {
				return errors.New("transaction will create a contract with value but empty code")
			}
			// No value submitted at least, critically Warn, but don't blow up
			return errors.New("transaction will create a contract with empty code")
		} else if txDataLen < 40 { // arbitrary heuristic limit
			return fmt.Errorf(
				"transaction will create a contract, but the payload is suspiciously small (%d bytes)",
				txDataLen,
			)
		}
	}

	// Not a contract creation, validate as a plain transaction
	if tx.To() != nil {
		to := common.NewMixedcaseAddress(*tx.To())
		if !to.ValidChecksum() {
			return errors.New("invalid checksum on recipient address")
		}

		if bytes.Equal(tx.To().Bytes(), common.Address{}.Bytes()) {
			return errors.New("transaction recipient is the zero address")
		}

		// If the data is not empty, validate that it has the 4byte prefix and the rest divisible by 32 bytes
		if txDataLen > 0 {
			if txDataLen < 4 {
				return errors.New("transaction data is not valid ABI (missing the 4 byte call prefix)")
			}

			if n := txDataLen - 4; n%32 != 0 {
				return fmt.Errorf(
					"transaction data is not valid ABI (length should be a multiple of 32 (was %d))",
					n,
				)
			}
		}
	}

	return nil
}
