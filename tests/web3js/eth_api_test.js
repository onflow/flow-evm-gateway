const { Web3 } = require('web3')
const web3Utils = require('web3-utils')
const { assert } = require('chai')

const web3 = new Web3("http://localhost:8545")

let eoaAccount = web3.eth.accounts.privateKeyToAccount("0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442")
let fundedAmount = 5.0
let startBlockHeight = 3 // start block height after setup accounts

it('get chain ID', async() => {
    let chainID = await web3.eth.getChainId()
    assert.isDefined(chainID)
    assert.equal(chainID, 646n)
})

it('get block', async () => {
    let height = await web3.eth.getBlockNumber()
    assert.equal(height, startBlockHeight)

    let block = await web3.eth.getBlock(height)
    assert.notDeepEqual(block, {})
    assert.isString(block.hash)
    assert.isString(block.parentHash)
    assert.isString(block.logsBloom)

    let blockHash = await web3.eth.getBlock(block.hash)
    assert.deepEqual(block, blockHash)

    // get block count and uncle count
    let txCount = await web3.eth.getBlockTransactionCount(startBlockHeight)
    let uncleCount = await web3.eth.getBlockUncleCount(startBlockHeight)

    assert.equal(txCount, 1n)
    assert.equal(uncleCount, 0n)

    // get block transaction
    let tx = await web3.eth.getTransactionFromBlock(startBlockHeight, 0)
    assert.isNotNull(tx)
    assert.equal(tx.blockNumber, block.number)
    assert.equal(tx.blockHash, block.hash)
    assert.isString(tx.hash)

    // not existing transaction
    let no = await web3.eth.getTransactionFromBlock(startBlockHeight, 1)
    assert.isNull(no)
})

it('get balance', async() => {
    let wei = await web3.eth.getBalance(eoaAccount.address)
    assert.isNotNull(wei)

    let flow = web3Utils.fromWei(wei, 'ether')
    assert.equal(parseFloat(flow), fundedAmount)

    let weiAtBlock = await web3.eth.getBalance(eoaAccount.address, startBlockHeight)
    assert.equal(wei, weiAtBlock)
})