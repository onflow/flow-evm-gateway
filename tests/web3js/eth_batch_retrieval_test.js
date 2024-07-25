const { assert } = require('chai')
const conf = require('./config')
const web3 = conf.web3

it('should retrieve batch transactions', async () => {
    // this test relies on the setup of batched transactions found in ../e2e_web3js_test.go

    let latestHeight = await web3.eth.getBlockNumber()
    let block = await web3.eth.getBlock(latestHeight)

    let batchSize = 10
    assert.lengthOf(block.transactions, batchSize)

    let cumulativeGasUsed = 0n
    for (let i = 0; i < block.transactions.length; i++) {
        let tx = await web3.eth.getTransactionFromBlock(latestHeight, i)
        assert.equal(tx.blockNumber, block.number, "wrong block number")
        assert.equal(tx.blockHash, block.hash, "wrong block hash")
        assert.equal(tx.type, 0, "wrong type")
        assert.equal(tx.transactionIndex, i, "wrong index")
        assert.isBelow(i, batchSize, "wrong batch size")

        let receipt = await web3.eth.getTransactionReceipt(tx.hash)
        cumulativeGasUsed += receipt.gasUsed

        assert.equal(receipt.cumulativeGasUsed, cumulativeGasUsed)
    }
})
