const { Web3 } = require('web3')
const web3Utils = require('web3-utils')
const { assert } = require('chai')
const fs = require("fs")

const web3 = new Web3("http://localhost:8545")

const eoaAccount = web3.eth.accounts.privateKeyToAccount("0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442")
const fundedAmount = 5.0
const startBlockHeight = 3 // start block height after setup accounts
const serviceEOA = "0xfacf71692421039876a5bb4f10ef7a439d8ef61e" // configured account as gw service
const successStatus = 1n

// deployContract deploys a contract by name, the contract files must be saved in
// fixtures folder, each contract must have two files: ABI and bytecode,
// the ABI file must be named {name}ABI.json and contain ABI definition for the contract
// and bytecode file must be named {name}.byte and must contain compiled byte code of the contract.
async function deployContract(name) {
    const storageABI = require(`../fixtures/${name}ABI.json`)
    let storageCode = await fs.promises.readFile(`../fixtures/${name}.byte`, 'utf8')

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
