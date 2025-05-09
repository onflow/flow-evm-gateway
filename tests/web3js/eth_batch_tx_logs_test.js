const { assert } = require('chai')
const conf = require('./config')
const web3 = conf.web3

it('should retrieve batch transactions with logs', async () => {
    // this test relies on the setup of batched transactions with logs found in ../e2e_web3js_test.go

    let latestHeight = await web3.eth.getBlockNumber()
    let block = await web3.eth.getBlock(latestHeight)

    let batchSize = 6
    assert.lengthOf(block.transactions, batchSize)

    let blockLogs = []
    let logsBlooms = []

    for (let i = 0; i < block.transactions.length; i++) {
        let tx = await web3.eth.getTransactionFromBlock(latestHeight, i)
        let receipt = await web3.eth.getTransactionReceipt(tx.hash)
        assert.equal(receipt.blockNumber, block.number, 'wrong block number')
        assert.equal(receipt.blockHash, block.hash, 'wrong block hash')
        assert.equal(receipt.type, 0, 'wrong tx type')
        assert.equal(receipt.transactionIndex, i, 'wrong tx index')
        assert.isBelow(i, batchSize, 'wrong batch size')

        // the contract deployment transaction has no logs
        if (receipt.logs.length == 0) {
            continue
        }

        for (const log of receipt.logs) {
            assert.equal(log.blockNumber, block.number, 'wrong block number')
            assert.equal(log.blockHash, block.hash, 'wrong block hash')
            assert.equal(log.transactionHash, tx.hash, 'wrong tx hash')
            assert.equal(log.transactionIndex, i, 'wrong tx index')
            // the 1st transaction contains the contract deployment
            assert.equal(log.logIndex, log.transactionIndex - 1n, 'wrong log index')
            assert.isFalse(log.removed, 'log should not be removed')
            assert.equal(log.address, '0x99a64c993965f8d69f985b5171bc20065cc32fab', 'wrong log address')
            assert.equal(log.topics.length, 4, 'wrong topics length')
            assert.equal(log.data.length, 66, 'wrong data length')

            blockLogs.push(log)
        }
        logsBlooms.push(receipt.logsBloom)
    }

    let expectedBlockLogsBloom = '0x00000000008000000000004000040000000000000000000000000000000000000002000000000800080000000420000000012200040000000020000000080000000000000000008000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000010001000000000010000000000010000000080000000000000000400000000000000000000000000000100000020000000000000000000000000000002000000000000000000000000800000000000000000000000000020000000000000000028000800000000000000000000080000400000000000000000000000008000000000000000'
    assert.equal(
        block.logsBloom,
        expectedBlockLogsBloom
    )

    for (const logsBloom of logsBlooms) {
        assert.notEqual(
            logsBloom,
            expectedBlockLogsBloom
        )
    }

    let logs = await web3.eth.getPastLogs({
        fromBlock: block.number,
        toBlock: block.number
    })

    for (let i = 0; i < logs; i++) {
        assert.deepEqual(logs[i], blockLogs[i])
    }
})
