import EVM

access(all)
fun main(hexEncodedData: String, hexEncodedAddress: String): String {
    let account = getAuthAccount<auth(Storage) &Account>(Address(0xCOA))

    let coa = account.storage.borrow<auth(EVM.Call) &EVM.CadenceOwnedAccount>(
        from: /storage/evm
    ) ?? panic("Could not borrow reference to the COA!")
    let addressBytes = hexEncodedAddress.decodeHex().toConstantSized<[UInt8; 20]>()!

    let callResult = coa.call(
        to: EVM.EVMAddress(bytes: addressBytes),
        data: hexEncodedData.decodeHex(),
        gasLimit: 15000000, // todo make it configurable, max for now
        value: EVM.Balance(attoflow: 0)
    )

    return String.encodeHex(callResult.data)
}
