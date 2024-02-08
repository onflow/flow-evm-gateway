import EVM from 0xf8d6e0586b0a20c7

access(all)
fun main(addressBytes: [UInt8; 20]): UFix64 {
    let address = EVM.EVMAddress(bytes: addressBytes)

    return address.balance().flow
}
