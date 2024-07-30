const { Web3 } = require('web3')

const web3 = new Web3("http://127.0.0.1:8545")

module.exports = {
    web3: web3,
    eoa: web3.eth.accounts.privateKeyToAccount("0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442"),
    fundedAmount: 5.0,
    startBlockHeight: 2n, // start block height after setup accounts
    serviceEOA: "0xfacf71692421039876a5bb4f10ef7a439d8ef61e", // configured account as gw service
    successStatus: 1n,
    minGasPrice: 150n
}
