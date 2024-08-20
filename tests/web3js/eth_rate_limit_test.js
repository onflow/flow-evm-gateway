const { assert } = require('chai')
const {Web3} = require("web3")

it('rate limit after X requests', async function () {
    this.timeout(0)
    setTimeout(() => process.exit(0), 5000) // make sure the process exits
    let ws = new Web3("ws://127.0.0.1:8545")

    // wait for ws connection to establish and reset rate-limit timer
    await new Promise(res => setTimeout(res, 1500))

    // this should be synced with the value on server config
    let requestLimit = 50
    let requestsMade = 0
    let requestsFailed = 0
    let requests = 60

    for (let i = 0; i < requests; i++) {
        try {
            await ws.eth.getBlockNumber()
            requestsMade++
        } catch(e) {
            assert.equal(e.innerError.message, 'limit of requests per second reached')
            requestsFailed++
        }
    }

    assert.equal(requestsMade, requestLimit, "more requests made than the limit")
    assert.equal(requestsFailed, requests-requestLimit, "failed requests don't match expected value")

    await new Promise(res => setTimeout(res, 1000))

    // after 1 second which is the rate-limit interval we can repeat
    requestsMade = 0
    requestsFailed = 0

    for (let i = 0; i < requests; i++) {
        try {
            await ws.eth.getBlockNumber()
            requestsMade++
        } catch(e) {
            assert.equal(e.innerError.message, 'limit of requests per second reached')
            requestsFailed++
        }
    }

    assert.equal(requestsMade, requestLimit, "more requests made than the limit")
    assert.equal(requestsFailed, requests-requestLimit, "failed requests don't match expected value")

    await ws.currentProvider.disconnect()
})
