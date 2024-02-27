import EVM

access(all)
fun main(addressBytes: [UInt8; 20]): UInt {
    let address = EVM.EVMAddress(bytes: addressBytes)

    return address.balance().inAttoFLOW()
}
