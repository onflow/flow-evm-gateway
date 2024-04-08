const utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const {Web3} = require("web3");
const {startBlockHeight} = require("./config");
const {waitWithTimeout, rejectIfConditionAtInterval} = require("web3-utils");
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
    await sub.sendSubscriptionRequest()

    // todo figure out why I have subscribe error: {"code":-32601,"message":"notifications not supported"}
    // request:
    // {"level":"debug","component":"API","url":"/","id":"24c0dbde-1999-484b-a762-8075ea5a7f02","jsonrpc":"2.0","method":"eth_subscribe","params":["logs",{"address":"0x99A64c993965f8d69F985b5171bC20065Cc32fAB","topics":["0x76efea95e5da1fa661f235b2921ae1d89b99e457ec73fb88e34a1d150f95c64b",null,null,null]}],"is-ws":false,"time":"2024-04-05T19:00:51Z","message":"API request"}

    // todo add pulling of new data test

    sub.on("connected", function(subscriptionId){
        console.log("subscription", subscriptionId)
    })

    sub.on('data', function(event){
        console.log("data")
        console.log(event)
        done()
    })

    sub.on('error', function(error, receipt) {
        console.log("error", err)
    })
     */

    let ws = new Web3("ws://localhost:8545")

    // get all the new blocks
    let doneBlocks = new Promise(async (res, rej) => {
        let eventCount = 0
        let sub = await ws.eth.subscribe('newBlockHeaders')
        sub.on('data', async block => {
            // todo assert

            if (++eventCount === testValues.length) {
                await sub.unsubscribe()
                res()
            }
        })
    })

    // get all pending transactions
    let doneTxs = new Promise(async (res, rej) => {
        let eventCount = 0
        let sub = await ws.eth.subscribe('pendingTransactions')
        sub.on('data', async tx => {
            console.log("TX", tx)

            if (++eventCount === testValues.length) {
                await sub.unsubscribe()
                res()
            }
        })
    })

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
    }

    // todo the problem is transactions can be ingested before blocks events, which mean the tx event can be broadcasted and when block is fetched at that height it doesn't yet exists in db

    // wait for all events to be received
    await doneTxs
    await doneBlocks

}).timeout(15*1000)