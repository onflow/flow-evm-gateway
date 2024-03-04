import EVM

access(all)
fun main(hexEncodedAddress: String): UInt {
    let addressBytes = hexEncodedAddress.decodeHex().toConstantSized<[UInt8; 20]>()!
    let address = EVM.EVMAddress(bytes: addressBytes)

    return address.balance().inAttoFLOW()
}
