import EVM

access(all)
fun main(hexEncodedAddress: String): UInt {
    let address = EVM.addressFromString(hexEncodedAddress)

    return address.balance().inAttoFLOW()
}
