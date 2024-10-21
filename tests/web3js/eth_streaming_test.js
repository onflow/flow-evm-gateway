const conf = require('./config')
const helpers = require('./helpers')
const { assert } = require('chai')
const { Web3 } = require('web3')

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

    // subscribe to new blocks being produced by bellow transaction submission
    let blockTxHashes = []
    let subBlocks = await ws.eth.subscribe('newBlockHeaders')
    subBlocks.on('error', async (err) => {
        assert.fail(err.message)
    })
    subBlocks.on('data', async (block) => {
        blockTxHashes.push(block.transactions[0]) // add received tx hash

        if (blockTxHashes.length === testValues.length) {
            subBlocks.unsubscribe()
        }
    })

    // subscribe to all new transaction events being produced by transaction submission bellow
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

    // subscribe to events being emitted by a deployed contract and bellow transaction interactions
    let logTxHashes = []
    let subLog = await ws.eth.subscribe('logs', {
        address: contractAddress,
    })
    subLog.on('error', async err => {
        assert.fail(err.message)
    })
    subLog.on('data', async (log) => {
        logTxHashes.push(log.transactionHash)

        if (logTxHashes.length === testValues.length) {
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
    // match array of transaction hashes received from events for blocks and txs
    assert.deepEqual(blockTxHashes, sentHashes)
    assert.deepEqual(txHashes, sentHashes)
    assert.deepEqual(logTxHashes, sentHashes)
})
