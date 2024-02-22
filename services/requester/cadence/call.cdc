import EVM

access(all)
fun main(data: [UInt8], contractAddress: [UInt8; 20]): [UInt8] {
    let account = getAuthAccount<auth(Storage) &Account>(Address(0xCOA))

    let coa = account.storage.borrow<&EVM.CadenceOwnedAccount>(from: /storage/evm)
        ?? panic("Could not borrow reference to the COA!")

    return coa.call(
        to: EVM.EVMAddress(bytes: contractAddress),
        data: data,
        gasLimit: 15000000, // todo make it configurable, max for now
        value: EVM.Balance(attoflow: 0)
    )
}
