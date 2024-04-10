const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const {Web3} = require("web3");
const web3 = conf.web3

it('streaming of logs using filters', async() => {
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

    // subscribe to events
    /*
    const sub = await deployed.contract.events.Calculated()
     */

    let ws = new Web3("ws://localhost:8545")

    let blockCount = 0
    let blockHashes = []
    // get all the new blocks
    let doneBlocks = new Promise(async (res, rej) => {
        let subBlocks = await ws.eth.subscribe('newBlockHeaders')
        subBlocks.on("connected", id => console.log("blocks subscribed, id: ", id))
        subBlocks.on('error', err => console.log("blocks subscription error: ", err))

        subBlocks.on('data', async block => {
            blockHashes.push(block.transactions[0]) // add received tx hash

            blockCount++
            if (blockCount === testValues.length) {
                let val = await subBlocks.unsubscribe()
                res()
            }
        })
    })

    let txCount = 0
    let txHashes = []
    // get all pending transactions
    let doneTxs = new Promise(async (res, rej) => {
        let subTx = await ws.eth.subscribe('pendingTransactions')
        subTx.on("connected", id => console.log("tx subscribed, id: ", id))
        subTx.on('error', err => console.log("tx subscription error: ", err))

        subTx.on('data', async tx => {
            txHashes.push(tx) // add received tx hash
            txCount++
            if (txCount === testValues.length) {
                let val = await subTx.unsubscribe()
                res()
            }
        })
    })

    // wait for subscription for a bit
    await new Promise((res, rej) => setTimeout(() => res(), 300))

    let sentHashes = []
    // produce events
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
    await Promise.all([doneTxs, doneBlocks])

    // check that transaction hashes we received when submitting transactions above
    // match array of transaction hashes received from events for blocks and txs
    assert.deepEqual(blockHashes, sentHashes)
    assert.deepEqual(txHashes, sentHashes)

}).timeout(20*1000)