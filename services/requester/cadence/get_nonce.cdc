import EVM

access(all)
fun main(addressBytes: [UInt8; 20]): UInt64 {
    let address = EVM.EVMAddress(bytes: addressBytes)

    return address.nonce()
}
