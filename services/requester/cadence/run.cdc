import EVM

transaction(hexEncodedTx: String, coinbase: String) {
    execute {
        let txResult = EVM.run(
            tx: hexEncodedTx.decodeHex(),
            coinbase: EVM.addressFromString(coinbase)
        )
        assert(
            txResult.status == EVM.Status.failed || txResult.status == EVM.Status.successful,
            message: "evm_error=".concat(txResult.errorMessage).concat("\n")
        )
    }
}
