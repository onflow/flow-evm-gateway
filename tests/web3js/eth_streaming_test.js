const conf = require('./config')
const helpers = require('./helpers')
const { assert } = require('chai')
const { Web3 } = require('web3')
const web3 = conf.web3

it('streaming of blocks, transactions, logs using filters', async () => {
    // this is a failsafe if socket is kept open since test node process won't finish otherwise
    setTimeout(() => process.exit(1), 1000 * 25)

    let deployed = await helpers.deployContract('storage')
    let contractAddress = deployed.receipt.contractAddress

    let repeatA = 10
    const testValues = [
        { A: 1, B: 2 },
        { A: -1, B: -2 },
        { A: repeatA, B: 200 },
        { A: repeatA, B: 300 },
        { A: repeatA, B: 400 },
    ]

    let ws = new Web3('ws://127.0.0.1:8545')

    // wait for subscription for a bit
    await new Promise((res, rej) => setTimeout(() => res(), 1000))

    // subscribe to new blocks being produced by transaction submissions below
    let blocksHeaders = []
    let subBlocks = await ws.eth.subscribe('newBlockHeaders')
    subBlocks.on('error', async (err) => {
        assert.fail(err.message)
    })
    subBlocks.on('data', async (block) => {
        blocksHeaders.push(block) // add received tx hash

        if (blocksHeaders.length === testValues.length) {
            subBlocks.unsubscribe()
        }
    })

    // subscribe to all new transaction events being produced by transaction submissions below
    let txHashes = []
    let subTx = await ws.eth.subscribe('pendingTransactions')
    subTx.on('error', async (err) => {
        assert.fail(err.message)
    })
    subTx.on('data', async (tx) => {
        txHashes.push(tx) // add received tx hash

        if (txHashes.length === testValues.length) {
            subTx.unsubscribe()
        }
    })

    // subscribe to events being emitted by a deployed contract and transaction interactions below
    let logs = []
    let subLog = await ws.eth.subscribe('logs', {
        address: contractAddress,
    })
    subLog.on('error', async err => {
        assert.fail(err.message)
    })
    subLog.on('data', async (log) => {
        logs.push(log)

        if (logs.length === testValues.length) {
            subLog.unsubscribe()
        }
    })

    let sentHashes = []
    // produce events by submitting transactions
    for (const { A, B } of testValues) {
        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(A, B).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })
        assert.equal(res.receipt.status, conf.successStatus)
        sentHashes.push(res.receipt.transactionHash) // add sent hash
    }

    // wait for subscription for a bit
    await new Promise((res, rej) => setTimeout(() => res(), 1000))

    // check that transaction hashes we received when submitting transactions above
    // match array of transaction hashes received from subscriptions
    assert.deepEqual(txHashes, sentHashes)

    assert.lengthOf(blocksHeaders, testValues.length)
    for (let blockHeader of blocksHeaders) {
        let block = await web3.eth.getBlock(blockHeader.number)

        assert.equal(blockHeader.number, block.number)
        assert.equal(blockHeader.hash, block.hash)
        assert.equal(blockHeader.parentHash, block.parentHash)
        assert.equal(blockHeader.nonce, block.nonce)
        assert.equal(blockHeader.sha3Uncles, block.sha3Uncles)
        assert.equal(blockHeader.logsBloom, block.logsBloom)
        assert.equal(blockHeader.transactionsRoot, block.transactionsRoot)
        assert.equal(blockHeader.stateRoot, block.stateRoot)
        assert.equal(blockHeader.receiptsRoot, block.receiptsRoot)
        assert.equal(blockHeader.miner, block.miner)
        assert.equal(blockHeader.extraData, block.extraData)
        assert.equal(blockHeader.gasLimit, block.gasLimit)
        assert.equal(blockHeader.gasUsed, block.gasUsed)
        assert.equal(blockHeader.timestamp, block.timestamp)
        assert.equal(blockHeader.difficulty, block.difficulty)
    }

    assert.lengthOf(logs, testValues.length)
    for (let log of logs) {
        let matchingLogs = await web3.eth.getPastLogs({
            address: log.address,
            blockHash: log.blockHash
        })
        assert.lengthOf(matchingLogs, 1)
        assert.deepEqual(log, matchingLogs[0])
    }
})
