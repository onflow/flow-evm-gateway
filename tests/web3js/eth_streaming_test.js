const conf = require('./config')
const helpers = require('./helpers')
const { assert } = require('chai')
const { Web3 } = require("web3");

it('streaming of blocks, transactions, logs using filters', async () => {
    let deployed = await helpers.deployContract("storage")
    let contractAddress = deployed.receipt.contractAddress

    let repeatA = 10
    const testValues = [
        { A: 1, B: 2 },
        { A: -1, B: -2 },
        { A: repeatA, B: 200 },
        { A: repeatA, B: 300 },
        { A: repeatA, B: 400 },
    ]

    let ws = new Web3("ws://127.0.0.1:8545")

    // wait for subscription for a bit
    await new Promise((res, rej) => setTimeout(() => res(), 1000))

    // subscribe to new blocks being produced by bellow transaction submission
    let blockTxHashes = []
    let subBlocks = await ws.eth.subscribe('newBlockHeaders')
    let doneBlocks = new Promise(async (res, rej) => {
        subBlocks.on("error", err => {
            rej(err)
        })

        subBlocks.on('data', async block => {
            blockTxHashes.push(block.transactions[0]) // add received tx hash

            if (blockTxHashes.length === testValues.length) {
                subBlocks.unsubscribe()
                res()
            }
        })
    })

    // subscribe to all new transaction events being produced by transaction submission bellow
    let txHashes = []
    let subTx = await ws.eth.subscribe('pendingTransactions')
    let doneTxs = new Promise(async (res, rej) => {
        subTx.on("error", err => {
            rej(err)
        })

        subTx.on('data', async tx => {
            txHashes.push(tx) // add received tx hash

            if (txHashes.length === testValues.length) {
                subTx.unsubscribe()
                res()
            }
        })
    })

    let logTxHashes = []
    let subLog = await ws.eth.subscribe('logs', {
        address: contractAddress,
    })
    // subscribe to events being emitted by a deployed contract and bellow transaction interactions
    let doneAddressLogs = new Promise(async (res, rej) => {
        subLog.on("error", err => {
            rej(err)
        })

        subLog.on('data', async (data) => {
            logTxHashes.push(data.transactionHash)

            if (logTxHashes.length === testValues.length) {
                subLog.unsubscribe()
                res()
            }
        })
    })

    // wait for subscription for a bit
    await new Promise((res, rej) => setTimeout(() => res(), 1000))

    let sentHashes = []
    // produce events by submitting transactions
    for (const { A, B } of testValues) {
        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(A, B).encodeABI(),
            gas: 1000000,
            gasPrice: 0
        })
        assert.equal(res.receipt.status, conf.successStatus)
        sentHashes.push(res.receipt.transactionHash) // add sent hash
    }

    // wait for all events to be received
    await Promise.all([doneTxs, doneBlocks, doneAddressLogs])

    // check that transaction hashes we received when submitting transactions above
    // match array of transaction hashes received from events for blocks and txs
    assert.deepEqual(blockTxHashes, sentHashes)
    assert.deepEqual(txHashes, sentHashes)
    assert.deepEqual(logTxHashes, sentHashes)

    process.exit(0)
})
