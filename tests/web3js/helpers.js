const fs = require("fs")
const conf = require("./config")
const chai = require("chai")
const chaiHttp = require('chai-http')
const web3 = conf.web3

chai.use(chaiHttp);

// deployContract deploys a contract by name, the contract files must be saved in
// fixtures folder, each contract must have two files: ABI and bytecode,
// the ABI file must be named {name}ABI.json and contain ABI definition for the contract
// and bytecode file must be named {name}.byte and must contain compiled byte code of the contract.
//
// Returns the contract object as well as the receipt deploying the contract.
async function deployContract(name) {
  const abi = require(`../fixtures/${name}ABI.json`)
  const code = await fs.promises.readFile(`${__dirname}/../fixtures/${name}.byte`, 'utf8')
  const contractABI = new web3.eth.Contract(abi, { handleReverted: true })

  let data = contractABI
    .deploy({ data: `0x${code}` })
    .encodeABI()

  let signed = await conf.eoa.signTransaction({
    from: conf.eoa.address,
    data: data,
    value: '0',
    gasPrice: conf.minGasPrice,
  })

  let receipt = await web3.eth.sendSignedTransaction(signed.rawTransaction)

  return {
    contract: new web3.eth.Contract(abi, receipt.contractAddress, { handleReverted: true }),
    receipt: receipt
  }
}

async function deployContractFrom(from, name) {
  const abi = require(`../fixtures/${name}ABI.json`)
  const code = await fs.promises.readFile(`${__dirname}/../fixtures/${name}.byte`, 'utf8')
  const contractABI = new web3.eth.Contract(abi, { handleReverted: true })

  let data = contractABI
    .deploy({ data: `0x${code}` })
    .encodeABI()

  let signed = await from.signTransaction({
    from: from.address,
    data: data,
    value: '0',
    gasPrice: conf.minGasPrice,
  })

  let receipt = await web3.eth.sendSignedTransaction(signed.rawTransaction)

  return {
    contract: new web3.eth.Contract(abi, receipt.contractAddress, { handleReverted: true }),
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

// signAndSendFrom signs a transactions from the given EOA and submits it
// to the network, returning a transaction hash and receipt
async function signAndSendFrom(from, tx) {
  const signedTx = await from.signTransaction(tx)
  // send transaction and make sure interaction was success
  const receipt = await web3.eth.sendSignedTransaction(signedTx.rawTransaction)

  return {
    hash: signedTx.transactionHash,
    receipt: receipt,
  }
}

// callRPCMethod accepts a method name and its params and
// makes a POST request to the JSON-RPC API server.
// Returns a promise for the response.
async function callRPCMethod(methodName, params) {
  return chai.request('http://127.0.0.1:8545')
    .post('/')
    .set('Content-Type', 'application/json')
    .set('Accept', 'application/json')
    .send({
      'jsonrpc': '2.0',
      'method': methodName,
      'id': 1,
      'params': params
    })
}

exports.signAndSend = signAndSend
exports.deployContract = deployContract
exports.deployContractFrom = deployContractFrom
exports.callRPCMethod = callRPCMethod
exports.signAndSendFrom = signAndSendFrom
