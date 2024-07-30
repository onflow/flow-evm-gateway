const web3Utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const web3 = conf.web3

it('get chain ID', async () => {
    let chainID = await web3.eth.getChainId()

    assert.isDefined(chainID)
    assert.equal(chainID, 646n)
})

it('get block', async () => {
    let height = await web3.eth.getBlockNumber()
    assert.equal(height, conf.startBlockHeight)

    let block = await web3.eth.getBlock(height)
    assert.notDeepEqual(block, {})
    assert.isString(block.hash)
    assert.isString(block.parentHash)
    assert.lengthOf(block.logsBloom, 514)
    assert.isDefined(block.timestamp)
    assert.isTrue(block.timestamp >= 1714413860n)
    assert.notEqual(
        block.transactionsRoot,
        '0x0000000000000000000000000000000000000000000000000000000000000000'
    )
    assert.equal(block.size, 3960n)

    let blockHash = await web3.eth.getBlock(block.hash)
    assert.deepEqual(block, blockHash)

    // get block count and uncle count
    let txCount = await web3.eth.getBlockTransactionCount(conf.startBlockHeight)
    let uncleCount = await web3.eth.getBlockUncleCount(conf.startBlockHeight)

    assert.equal(txCount, 3n)
    assert.equal(uncleCount, 0n)

    // get block transactions
    for (const txIndex of [0, 1, 2]) {
        let tx = await web3.eth.getTransactionFromBlock(conf.startBlockHeight, txIndex)
        assert.isNotNull(tx)
        assert.equal(tx.blockNumber, block.number)
        assert.equal(tx.blockHash, block.hash)
        assert.isString(tx.hash)
        assert.equal(tx.transactionIndex, txIndex)
    }

    // not existing transaction
    let no = await web3.eth.getTransactionFromBlock(conf.startBlockHeight, 5)
    assert.isNull(no)
})

it('get earliest/genesis block', async () => {
    let block = await web3.eth.getBlock('earliest')

    assert.notDeepEqual(block, {})
    assert.equal(block.number, 0n)
    assert.isString(block.hash)
    assert.isString(block.parentHash)
    assert.lengthOf(block.logsBloom, 514)
    assert.isDefined(block.timestamp)
    assert.isTrue(block.timestamp >= 1714090657n)
    assert.isUndefined(block.transactions)
})

it('get block and transactions with COA interactions', async () => {
    let block = await web3.eth.getBlock(conf.startBlockHeight)
    assert.notDeepEqual(block, {})

    for (const txIndex of [0, 1]) {
        // get block transaction
        let tx = await web3.eth.getTransactionFromBlock(block.number, txIndex)
        // Assert that the transaction type is `0`, the type of `LegacyTx`.
        assert.equal(tx.type, 0n)
        assert.equal(tx.transactionIndex, txIndex)

        // get transaction receipt
        let receipt = await web3.eth.getTransactionReceipt(tx.hash)
        // Assert that the transaction type from receipt is `0`, the type of `LegacyTx`.
        assert.equal(receipt.type, 0n)
        if (receipt.contractAddress != null) {
            assert.equal(receipt.gasUsed, 702600n)
            assert.equal(receipt.cumulativeGasUsed, 702600n)
        } else {
            assert.equal(receipt.gasUsed, 21055n)
            assert.equal(receipt.cumulativeGasUsed, 723655n)
        }
    }

    // get block transaction
    let tx = await web3.eth.getTransactionFromBlock(2n, 0)
    assert.equal(tx.v, "0xff")
    assert.equal(tx.r, "0x0000000000000000000000000000000000000000000000020000000000000000")
    assert.equal(tx.s, "0x0000000000000000000000000000000000000000000000000000000000000004")

    tx = await web3.eth.getTransactionFromBlock(2n, 1)
    assert.equal(tx.v, "0xff")
    assert.equal(tx.r, "0x0000000000000000000000000000000000000000000000010000000000000000")
    assert.equal(tx.s, "0x0000000000000000000000000000000000000000000000000000000000000001")
})

it('get balance', async () => {
    let wei = await web3.eth.getBalance(conf.eoa.address)
    assert.isNotNull(wei)

    let flow = web3Utils.fromWei(wei, 'ether')
    assert.equal(parseFloat(flow), conf.fundedAmount)

    let weiAtBlock = await web3.eth.getBalance(conf.eoa.address, conf.startBlockHeight)
    assert.equal(wei, weiAtBlock)
})

it('get code', async () => {
    let code = await web3.eth.getCode(conf.eoa.address)
    assert.equal(code, "0x") // empty
})

it('get coinbase', async () => {
    let coinbase = await web3.eth.getCoinbase()
    assert.equal(coinbase, conf.serviceEOA) // e2e configured account
})

it('get gas price', async () => {
    let gasPrice = await web3.eth.getGasPrice()
    assert.equal(gasPrice, 0n) // 0 by default in tests
})

it('get transaction', async () => {
    let blockTx = await web3.eth.getTransactionFromBlock(conf.startBlockHeight, 2)
    assert.isNotNull(blockTx)

    let tx = await web3.eth.getTransaction(blockTx.hash)
    assert.deepEqual(blockTx, tx)
    assert.isString(tx.hash)
    assert.equal(tx.blockNumber, conf.startBlockHeight)
    assert.equal(tx.gas, 300000n)
    assert.isNotEmpty(tx.from)
    assert.isNotEmpty(tx.r)
    assert.isNotEmpty(tx.s)
    assert.equal(tx.transactionIndex, 2)

    let rcp = await web3.eth.getTransactionReceipt(tx.hash)
    assert.isNotEmpty(rcp)
    assert.equal(rcp.blockHash, blockTx.blockHash)
    assert.equal(rcp.blockNumber, conf.startBlockHeight)
    assert.equal(rcp.from, tx.from)
    assert.equal(rcp.to, tx.to)
    assert.equal(rcp.gasUsed, 21000n)
    assert.equal(rcp.cumulativeGasUsed, 744655n)
    assert.equal(rcp.transactionHash, tx.hash)
    assert.equal(rcp.status, conf.successStatus)
})

// it shouldn't fail, but return empty
it('get not found values', async () => {
    const nonExistingHeight = 9999999999

    assert.isNull(await web3.eth.getBlock(nonExistingHeight))
    assert.isNull(await web3.eth.getTransactionFromBlock(nonExistingHeight, 0))
})

it('get mining status', async () => {
    let mining = await web3.eth.isMining()
    assert.isFalse(mining)
})

it('get syncing status', async () => {
    let isSyncing = await web3.eth.isSyncing()
    assert.isFalse(isSyncing)
})

it('can make batch requests', async () => {
    let batch = new web3.BatchRequest()
    let getBlockNumber = {
        jsonrpc: '2.0',
        id: 1,
        method: 'eth_blockNumber',
        params: [],
    }
    let getChainId = {
        jsonrpc: '2.0',
        id: 2,
        method: 'eth_chainId',
        params: [],
    }
    let getSyncing = {
        jsonrpc: '2.0',
        id: 3,
        method: 'eth_syncing',
        params: [],
    }
    let getNetVersion = {
        jsonrpc: '2.0',
        id: 4,
        method: 'net_version',
        params: [],
    }
    let getBlockTransactionCount = {
        jsonrpc: '2.0',
        id: 5,
        method: 'eth_getBlockTransactionCountByNumber',
        params: ['0x2'],
    }

    batch.add(getBlockNumber)
    batch.add(getChainId)
    batch.add(getSyncing)
    batch.add(getNetVersion)
    batch.add(getBlockTransactionCount)

    let results = await batch.execute()

    assert.deepEqual(
        results[0],
        { jsonrpc: '2.0', id: 1, result: '0x2' }
    )
    assert.deepEqual(
        results[1],
        { jsonrpc: '2.0', id: 2, result: '0x286' }
    )
    assert.deepEqual(
        results[2],
        {
            jsonrpc: '2.0',
            id: 3,
            result: false
        }
    )
    assert.deepEqual(
        results[3],
        { jsonrpc: '2.0', id: 4, result: '646' }
    )
    assert.deepEqual(
        results[4],
        { jsonrpc: '2.0', id: 5, result: '0x3' }
    )

    // The maximum number of batch requests is 5,
    // so this next batch should fail.
    let getTransactionCount = {
        jsonrpc: '2.0',
        id: 6,
        method: 'eth_getTransactionCount',
        params: ['0x658Bdf435d810C91414eC09147DAA6DB62406379', 'latest'],
    }

    batch.add(getTransactionCount)

    try {
        results = await batch.execute()
    } catch (error) {
        assert.equal(error.innerError[0].message, 'batch too large')
    }
})

it('get fee history', async () => {
    let response = await web3.eth.getFeeHistory(10, 'latest', [20])

    assert.deepEqual(
        response,
        {
            oldestBlock: 1n,
            reward: [['0x0'], ['0x0']], // gas price is always 0 during testing
            baseFeePerGas: [0n, 0n],
            gasUsedRatio: [0, 0.04964366666666667]
        }
    )
})
