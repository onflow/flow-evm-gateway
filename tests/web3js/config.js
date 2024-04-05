const { Web3 } = require('web3')
const web3 = new Web3("http://localhost:8545")

const eoaAccount = web3.eth.accounts.privateKeyToAccount("0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442")
const fundedAmount = 5.0
const startBlockHeight = 3n // start block height after setup accounts
const serviceEOA = "0xfacf71692421039876a5bb4f10ef7a439d8ef61e" // configured account as gw service
const successStatus = 1n

exports.web3 = web3
exports.eoa = eoaAccount
exports.fundedAmount = fundedAmount
exports.startBlockHeight = startBlockHeight
exports.serviceEOA = serviceEOA
exports.successStatus = successStatus
