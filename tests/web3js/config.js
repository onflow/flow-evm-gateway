const { Web3 } = require('web3')

const web3 = new Web3("http://localhost:8545")

module.exports = {
    web3: web3,
    web3WS: new Web3("ws://localhost:8545"),
    eoa: web3.eth.accounts.privateKeyToAccount("0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442"),
    fundedAmount: 5.0,
    startBlockHeight: 3n, // start block height after setup accounts
    serviceEOA: "0xFACF71692421039876a5BB4F10EF7A439D8ef61E", // configured account as gw service
    successStatus: 1n
}
