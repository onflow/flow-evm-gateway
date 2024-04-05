const fs = require("fs")
const conf = require("./config")
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

exports.deployContract = deployContract