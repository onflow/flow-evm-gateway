const fs = require("fs")
const conf = require("./config")
const {assert} = require("chai");
const web3 = conf.web3

// deployContract deploys a contract by name, the contract files must be saved in
// fixtures folder, each contract must have two files: ABI and bytecode,
// the ABI file must be named {name}ABI.json and contain ABI definition for the contract
// and bytecode file must be named {name}.byte and must contain compiled byte code of the contract.
//
// Returns the contract object as well as the receipt deploying the contract.
async function deployContract(name) {
    const abi = require(`../fixtures/${name}ABI.json`)
    const code = await fs.promises.readFile(`${__dirname}/../fixtures/${name}.byte`, 'utf8')
    const contractABI = new web3.eth.Contract(abi)

    let data = contractABI
        .deploy({ data: `0x${code}` })
        .encodeABI()

    let signed = await conf.eoa.signTransaction({
        from: conf.eoa.address,
        data: data,
        value: '0',
        gasPrice: '0',
    })

    let receipt = await web3.eth.sendSignedTransaction(signed.rawTransaction)

    return {
        contract: new web3.eth.Contract(abi, receipt.contractAddress),
        receipt: receipt
    }
}

// signAndSend signs a transactions and submits it to the network,
// returning a transaction hash and receipt
async function signAndSend(tx) {
    const signedTx = await conf.eoa.signTransaction(tx)
    // send transaction and make sure interaction was success
    const receipt = await web3.eth.sendSignedTransaction(signedTx.rawTransaction)

    return {
        hash: signedTx.transactionHash,
        receipt: receipt,
    }
}

exports.signAndSend = signAndSend
exports.deployContract = deployContract