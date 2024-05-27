const conf = require('./config')
const helpers = require('./helpers')
const { assert } = require('chai')
const {Web3} = require("web3");

const timeout = 30 // test timeout seconds

it('streaming of logs using filters', async() => {
    setTimeout(() => process.exit(1), (timeout-1)*1000) // hack if the ws connection is not closed

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
    let blockCount = 0
    let blockHashes = []
    let doneBlocks = new Promise(async (res, rej) => {
        let subBlocks = await ws.eth.subscribe('newBlockHeaders')
        subBlocks.on('data', async block => {
            blockHashes.push(block.transactions[0]) // add received tx hash

            blockCount++
            if (blockCount === testValues.length) {
                await subBlocks.unsubscribe()
                res()
            }
        })
        subBlocks.on("error", console.log)
    })

    // subscribe to all new transaction events being produced by transaction submission bellow
    let txCount = 0
    let txHashes = []
    let doneTxs = new Promise(async (res, rej) => {
        let subTx = await ws.eth.subscribe('pendingTransactions')
        subTx.on('data', async tx => {
            txHashes.push(tx) // add received tx hash
            txCount++
            if (txCount === testValues.length) {
                await subTx.unsubscribe()
                res()
            }
        })
    })

    let logCount = 0
    let logHashes = []
    // subscribe to events being emitted by a deployed contract and bellow transaction interactions
    let doneAddressLogs = new Promise(async (res, rej) => {
        let subLog = await ws.eth.subscribe('logs', {
            address: contractAddress,
        })
        subLog.on('data', async (data) => {
            logHashes.push(data.transactionHash)
            logCount++
            if (logCount === testValues.length) {
                await subLog.unsubscribe()
                res()
            }
        })
    })

    // wait for subscription for a bit
    await new Promise((res, rej) => setTimeout(() => res(), 300))

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
    assert.deepEqual(blockHashes, sentHashes)
    assert.deepEqual(txHashes, sentHashes)
    assert.deepEqual(logHashes, sentHashes)

    await ws.eth.clearSubscriptions()

    process.exit(0)
}).timeout(timeout*1000)