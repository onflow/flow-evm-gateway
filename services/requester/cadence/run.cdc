import EVM

transaction(hexEncodedTx: String, coinbase: String) {
    execute {
        let txResult = EVM.run(
            tx: hexEncodedTx.decodeHex(),
            coinbase: EVM.addressFromString(coinbase)
        )

        // It is quite common for EVM transactions to be invalid due to nonce
        // mismatch (nonce too high / nonce too low). This is a user error,
        // as it is the sender's responsibility to sign the transaction with
        // the correct nonce.
        // We check if the error code is related to nonce mismatch, and simply
        // return early, before aborting the Cadence transaction.
        // This will reduce the error noise on the internal panels/metrics about
        // failed transaction rate etc.
        //
        // errorCode for "nonce too low" is 201
        // errorCode for "nonce too high" is 202
        if txResult.errorCode == 201 || txResult.errorCode == 202 {
            return
        }

        assert(
            txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful,
            message: "evm_error=".concat(txResult.errorMessage).concat("\n")
        )
    }
}
