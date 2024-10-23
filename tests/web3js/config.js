const { Web3 } = require('web3')
const web3 = new Web3('http://127.0.0.1:8545')

module.exports = {
    web3: web3,
    eoa: web3.eth.accounts.privateKeyToAccount('0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442'), // eoa is 0xfacf71692421039876a5bb4f10ef7a439d8ef61e
    fundedAmount: 5.0,
    startBlockHeight: 3n, // start block height after setup accounts
    coinbase: '0x658bdf435d810c91414ec09147daa6db62406379', // configured account to receive fees
    successStatus: 1n,
    minGasPrice: 150n
}
