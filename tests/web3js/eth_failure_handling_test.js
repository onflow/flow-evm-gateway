const { assert } = require('chai')
const helpers = require("./helpers")
const conf = require("./config")
const web3 = conf.web3

it('transfer failure due to too high nonce', async () => {
    let receiver = web3.eth.accounts.create()

    try {
        await helpers.signAndSend({
            from: conf.eoa.address,
            to: receiver.address,
            value: 1,
            gasPrice: '0',
            gasLimit: 55000,
            nonce: 1337, // invalid
        })
    } catch (e) {
        assert.include(e.message, "nonce too high")
        return
    }

    assert.fail("should not reach")
})

it('transfer failure due to too low nonce', async () => {
    let receiver = web3.eth.accounts.create()

    // increase nonce
    await helpers.signAndSend({
        from: conf.eoa.address,
        to: receiver.address,
        value: 1,
        gasPrice: '0',
        gasLimit: 55000,
    })

    try {
        await helpers.signAndSend({
            from: conf.eoa.address,
            to: receiver.address,
            value: 1,
            gasPrice: '0',
            gasLimit: 55000,
            nonce: 0, // invalid
        })
    } catch (e) {
        assert.include(e.message, "nonce too low")
        return
    }

    assert.fail("should not reach")
})