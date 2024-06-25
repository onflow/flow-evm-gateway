const { assert } = require('chai')
const conf = require('./config')
const web3 = conf.web3

it('should get net_version', async () => {
    let networkID = await web3.eth.net.getId()
    assert.equal(networkID, 646n)
})

it('should get net_listening', async () => {
    let isListening = await web3.eth.net.isListening()
    assert.isTrue(isListening)
})

it('should get net_peerCount', async () => {
    let peerCount = await web3.eth.net.getPeerCount()
    assert.equal(1n, peerCount)
})
