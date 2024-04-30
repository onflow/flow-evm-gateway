const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('retrieve batch transactions', async() => {
  let latestHeight = await web3.eth.getBlockNumber()
  let block = await web3.eth.getBlock(latestHeight)
  console.log(block)
  assert.lengthOf(block.transactions, 2)

  let deployTx = await web3.eth.getTransactionFromBlock(latestHeight, 0)
  console.log(deployTx)
  assert.equal(block.number, deployTx.blockNumber)
  assert.equal(block.hash, deployTx.blockHash)
  assert.equal(0, deployTx.type)
  assert.equal(0, deployTx.transactionIndex)

  let callTx = await web3.eth.getTransactionFromBlock(latestHeight, 1)
  console.log(callTx)
  assert.equal(block.number, callTx.blockNumber)
  assert.equal(block.hash, callTx.blockHash)
  assert.equal(0, callTx.type)
  assert.equal(1, callTx.transactionIndex)
})
