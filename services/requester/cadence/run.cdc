import EVM

transaction(hexEncodedTxs: [String], coinbase: String) {
    execute {
        let txs: [[UInt8]] = []
        for tx in hexEncodedTxs {
            txs.append(tx.decodeHex())
        }

        // If there's only one tx, use `EVM.run`.
        // If there are more, then use `EVM.batchRun`
        if txs.length == 1 {
            let txResult = EVM.run(
                tx: txs[0],
                coinbase: EVM.addressFromString(coinbase)
            )
            assert(
                txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful,
                message: "evm_error=\(txResult.errorMessage);evm_error_code=\(txResult.errorCode)"
            )
            return
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
        for txResult in txResults {
            assert(
                txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful,
                message: "evm_error=\(txResult.errorMessage);evm_error_code=\(txResult.errorCode)"
            )
        }
    }
}
