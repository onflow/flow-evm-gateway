const { Web3 } = require('web3')
const web3Utils = require('web3-utils')
const { assert } = require('chai')
const fs = require("fs");
const storageABI = require("../fixtures/storageABI.json");

const web3 = new Web3("http://localhost:8545")

const eoaAccount = web3.eth.accounts.privateKeyToAccount("0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442")
const fundedAmount = 5.0
const startBlockHeight = 3 // start block height after setup accounts
const serviceEOA = "0xfacf71692421039876a5bb4f10ef7a439d8ef61e" // configured account as gw service
const successStatus = 1n

it('deploy contract', async() => {



})

async function deployContract() {
    let storageCode = await fs.promises.readFile("./storage.byte", 'utf8')

    let counter = new web3.eth.Contract(storageABI)

    let data = counter
        .deploy({ data: `0x${storageCode}` })
        .encodeABI()

    let signed = await userAccount.signTransaction({
        from: userAccount.address,
        data: data,
        value: '0',
        gasPrice: '0',
    })

    return web3.eth.sendSignedTransaction(signed.rawTransaction)
}