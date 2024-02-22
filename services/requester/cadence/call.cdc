import EVM

access(all)
fun main(data: [UInt8], contractAddress: [UInt8; 20]): [UInt8] {
    let coa <- EVM.createCadenceOwnedAccount()

    let res = coa.call(
        to: EVM.EVMAddress(bytes: contractAddress),
        data: data,
        gasLimit: 300000, // TODO(m-Peter): Maybe also pass this as script argument
        value: EVM.Balance(attoflow: 0)
    )
    destroy coa
    return res
}
