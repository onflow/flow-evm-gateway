import EVM

access(all)
fun main(data: [UInt8], contractAddress: [UInt8; 20]): [UInt8] {
    // TODO(m-Peter): Pass Flow address of bridged account as script argument
    let account = getAuthAccount<auth(Storage) &Account>(Address(0xf8d6e0586b0a20c7))
    let bridgedAccount = account.storage.borrow<&EVM.BridgedAccount>(
        from: /storage/evm
    ) ?? panic("Could not borrow reference to the bridged account!")

    let evmResult = bridgedAccount.call(
        to: EVM.EVMAddress(bytes: contractAddress),
        data: data,
        gasLimit: 300000, // TODO(m-Peter): Maybe also pass this as script argument
        value: EVM.Balance(attoflow: 0)
    )

    return evmResult
}
