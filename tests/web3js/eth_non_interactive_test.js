const web3Utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3types = require('web3-types')
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
    assert.equal(block.size, 4028n)
    assert.equal(block.gasLimit, 120000000n)
    assert.equal(block.miner, '0x0000000000000000000000030000000000000000')
    assert.equal(
        block.sha3Uncles,
        '0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347'
    )
    assert.equal(
        block.stateRoot,
        '0x0000000000000000000000000000000000000000000000000000000000000000'
    )

    let blockHash = await web3.eth.getBlock(block.hash)
    assert.deepEqual(block, blockHash)

    // get block count and uncle count
    let txCount = await web3.eth.getBlockTransactionCount(conf.startBlockHeight)
    let uncleCount = await web3.eth.getBlockUncleCount(conf.startBlockHeight)

    assert.equal(txCount, 3n)
    assert.equal(uncleCount, 0n)

    let gasUsed = 0n
    // get block transactions & receipts
    for (const txIndex of [0, 1, 2]) {
        let tx = await web3.eth.getTransactionFromBlock(conf.startBlockHeight, txIndex)
        assert.isNotNull(tx)
        assert.equal(tx.blockNumber, block.number)
        assert.equal(tx.blockHash, block.hash)
        assert.isString(tx.hash)
        assert.equal(tx.transactionIndex, txIndex)

        let txReceipt = await web3.eth.getTransactionReceipt(tx.hash)
        assert.isNotNull(txReceipt)
        assert.equal(txReceipt.blockNumber, block.number)
        assert.equal(txReceipt.blockHash, block.hash)
        assert.isString(txReceipt.transactionHash)
        assert.equal(txReceipt.transactionIndex, txIndex)
        assert.isNotNull(txReceipt.from)

        // first transaction is the COA resource creation,
        // which also does the contract deployment
        if (txIndex == 0) {
            assert.isNotNull(txReceipt.contractAddress)
            assert.isUndefined(txReceipt.to)
        } else {
            assert.isUndefined(txReceipt.contractAddress)
            assert.isNotNull(txReceipt.to)
        }

        gasUsed += txReceipt.gasUsed
    }

    assert.equal(block.gasUsed, gasUsed)

    // not existing transaction
    let no = await web3.eth.getTransactionFromBlock(conf.startBlockHeight, 5)
    assert.isNull(no)
})

it('should get block receipts', async () => {
    let height = await web3.eth.getBlockNumber()
    assert.equal(height, conf.startBlockHeight)

    let block = await web3.eth.getBlock(height)
    let response = await helpers.callRPCMethod('eth_getBlockReceipts', [block.hash])
    assert.equal(response.status, 200)

    let blockReceipts = response.body.result
    assert.lengthOf(blockReceipts, 3)

    for (let blockReceipt of blockReceipts) {
        let txReceipt = await web3.eth.getTransactionReceipt(
            blockReceipt.transactionHash,
            web3types.ETH_DATA_FORMAT
        )
        // normalize missing fields from transaction receipt
        if (txReceipt.to === undefined) {
            txReceipt.to = null
        }
        if (txReceipt.contractAddress === undefined) {
            txReceipt.contractAddress = null
        }

        assert.deepEqual(blockReceipt, txReceipt)
    }
})

it('should get block transaction count', async () => {
    // call endpoint with block number
    let txCount = await web3.eth.getBlockTransactionCount(conf.startBlockHeight)
    assert.equal(txCount, 3n)

    // call endpoint with block hash
    let block = await web3.eth.getBlock(conf.startBlockHeight)
    txCount = await web3.eth.getBlockTransactionCount(block.hash)
    assert.equal(txCount, 3n)

    // call endpoint with 'earliest'
    txCount = await web3.eth.getBlockTransactionCount('earliest')
    assert.equal(txCount, 0n)

    // call endpoint with 'latest'
    txCount = await web3.eth.getBlockTransactionCount('latest')
    assert.equal(txCount, 3n)
})

it('should get transactions from block', async () => {
    // call endpoint with block number
    for (const txIndex of [0, 1, 2]) {
        let tx = await web3.eth.getTransactionFromBlock(conf.startBlockHeight, txIndex)
        assert.isNotNull(tx)
        assert.equal(tx.blockNumber, conf.startBlockHeight)
        assert.equal(tx.transactionIndex, txIndex)
    }

    // call endpoint with block hash
    let block = await web3.eth.getBlock(conf.startBlockHeight)
    for (const txIndex of [0, 1, 2]) {
        let tx = await web3.eth.getTransactionFromBlock(block.hash, txIndex)
        assert.isNotNull(tx)
        assert.equal(tx.blockHash, block.hash)
        assert.equal(tx.transactionIndex, txIndex)
    }

    // call endpoint with 'earliest'
    let tx = await web3.eth.getTransactionFromBlock('earliest', 0)
    assert.isNull(tx)

    // call endpoint with 'latest'
    for (const txIndex of [0, 1, 2]) {
        let tx = await web3.eth.getTransactionFromBlock('latest', txIndex)
        assert.isNotNull(tx)
        assert.equal(tx.blockNumber, conf.startBlockHeight)
        assert.equal(tx.blockHash, block.hash)
        assert.equal(tx.transactionIndex, txIndex)
    }
})

it('get earliest/genesis block', async () => {
    let block = await web3.eth.getBlock('earliest')

    assert.notDeepEqual(block, {})
    assert.equal(block.number, 0n)
    assert.isString(block.hash)
    assert.isString(block.parentHash)
    assert.lengthOf(block.logsBloom, 514)
    assert.isDefined(block.timestamp)
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
    let tx = await web3.eth.getTransactionFromBlock(conf.startBlockHeight, 0)
    assert.equal(tx.v, "0xff")
    assert.equal(tx.r, "0x0000000000000000000000000000000000000000000000020000000000000000")
    assert.equal(tx.s, "0x0000000000000000000000000000000000000000000000000000000000000004")

    tx = await web3.eth.getTransactionFromBlock(conf.startBlockHeight, 1)
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
    assert.equal(coinbase, conf.coinbase) // e2e configured account
})

it('get gas price', async () => {
    let gasPrice = await web3.eth.getGasPrice()
    assert.equal(gasPrice, conf.minGasPrice)
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
        params: ['0x3'],
    }

    batch.add(getBlockNumber)
    batch.add(getChainId)
    batch.add(getSyncing)
    batch.add(getNetVersion)
    batch.add(getBlockTransactionCount)

    let results = await batch.execute()

    assert.deepEqual(
        results[0],
        { jsonrpc: '2.0', id: 1, result: '0x3' }
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

    for (let i = 0; i <= 50; i++) {
        let getTransactionCount = {
            jsonrpc: '2.0',
            id: 6 + i,
            method: 'eth_getTransactionCount',
            params: ['0x658Bdf435d810C91414eC09147DAA6DB62406379', 'latest'],
        }
        batch.add(getTransactionCount)
    }

    let error = null
    try {
        // The maximum number of batch requests is 25,
        // so this next batch should fail.
        results = await batch.execute()
    } catch (err) {
        error = err
    }
    assert.equal(error.innerError[0].message, 'batch too large')
})

it('get fee history', async () => {
    let response = await web3.eth.getFeeHistory(10, 'latest', [20])

    assert.deepEqual(
        response,
        {
            oldestBlock: 1n,
            reward: [['0x96'], ['0x96'], ['0x96']], // gas price is 150 during testing
            baseFeePerGas: [1n, 1n, 1n],
            gasUsedRatio: [0, 0, 0.006205458333333334]
        }
    )
})
