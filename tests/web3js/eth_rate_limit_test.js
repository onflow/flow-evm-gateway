const conf = require('./config')
const { assert } = require('chai')
const {Web3} = require("web3")

it('rate limit after X requests', async () => {
    setTimeout(() => process.exit(0), 5000) // make sure the process exits
    let ws = new Web3("ws://127.0.0.1:8545")

    let requestLimit = 20 // this should be synced with the value on server config
    let requestsMade = 0
    let requestsFailed = 0
    let requests = 40

    for (let i = 0; i < requests; i++) {
        try {
            await ws.eth.getBlockNumber()
            requestsMade++
        } catch(e) {
            requestsFailed++
        }
    }

    assert.equal(requestsMade, requestLimit, "more requests made than the limit")
    assert.equal(requestsFailed, requests-requestLimit, "failed requests don't match expected value")

    await new Promise(res => setTimeout(res, 1000))

    // after 1 second we can repeat
    requestsMade = 0
    requestsFailed = 0

    for (let i = 0; i < requests; i++) {
        try {
            await ws.eth.getBlockNumber()
            requestsMade++
        } catch(e) {
            requestsFailed++
        }
    }

    assert.equal(requestsMade, requestLimit, "more requests made than the limit")
    assert.equal(requestsFailed, requests-requestLimit, "failed requests don't match expected value")

    await ws.currentProvider.disconnect()
})
