import EVM

transaction(hexEncodedTxs: [String], coinbase: String) {
    execute {
        let txs: [[UInt8]] = []
        for tx in hexEncodedTxs {
            txs.append(tx.decodeHex())
        }

        let txResults = EVM.batchRun(
            txs: txs,
            coinbase: EVM.addressFromString(coinbase)
        )

        // If at least one of the EVM transactions in the batch is either
        // failed or successful, in other words not invalid, we let the
        // Cadence transaction succeed.
        for txResult in txResults {
            if txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful {
                return
            }
        }

        // Otherwise, all EVM transactions are invalid txs and can't be
        // executed (such as nonce too low).
        // In this case, we fail the Cadence transaction with the error
        // message from the first EVM transaction.
        var invalidTx: EVM.Result? = nil
        for txResult in txResults {
            if !(txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful) {
                invalidTx = txResult
                break
            }
        }

        if invalidTx != nil {
            assert(
                false,
                message: "evm_error=".concat(invalidTx?.errorMessage!).concat("\n")
            )
        }
    }
}
