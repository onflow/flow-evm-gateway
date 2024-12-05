const { assert } = require('chai')
const helpers = require('./helpers')
const conf = require("./config")
const web3 = conf.web3

it('should fail when nonce too high', async () => {
    let receiver = web3.eth.accounts.create()

    try {
        await helpers.signAndSend({
            from: conf.eoa.address,
            to: receiver.address,
            value: 1,
            gasPrice: conf.minGasPrice,
            gasLimit: 55_000,
            nonce: 1337, // invalid
        })
    } catch (e) {
        assert.include(e.message, 'nonce too high: tx nonce 1337, next nonce 0')
        return
    }

    assert.fail('should not reach')
})

it('should fail when nonce too low', async () => {
    let receiver = web3.eth.accounts.create()

    // increase nonce
    await helpers.signAndSend({
        from: conf.eoa.address,
        to: receiver.address,
        value: 1,
        gasPrice: conf.minGasPrice,
        gasLimit: 55_000,
    })

    try {
        await helpers.signAndSend({
            from: conf.eoa.address,
            to: receiver.address,
            value: 1,
            gasPrice: conf.minGasPrice,
            gasLimit: 55_000,
            nonce: 0, // invalid
        })
    } catch (e) {
        assert.include(e.message, 'nonce too low: next nonce 1, tx nonce 0')
        return
    }

    assert.fail('should not reach')
})

it('should fail when insufficient gas price', async () => {
    let receiver = web3.eth.accounts.create()

    try {
        await helpers.signAndSend({
            from: conf.eoa.address,
            to: receiver.address,
            value: 10,
            gasPrice: conf.minGasPrice - 50n, // non-accepted gasPrice
            gasLimit: 55_000,
        })
    } catch (e) {
        assert.include(e.message, 'the minimum accepted gas price for transactions is: 150')
        return
    }

    assert.fail('should not reach')
})

it('should fail when insufficient balance for transfer', async () => {
    let receiver = web3.eth.accounts.create()

    await helpers.signAndSend({
        from: conf.eoa.address,
        to: receiver.address,
        value: 10_000_000,
        gasPrice: conf.minGasPrice,
        gasLimit: 55_000,
    })

    let signedTx = await receiver.signTransaction({
        from: receiver.address,
        to: conf.eoa.address,
        value: 10_100_000,
        gasPrice: conf.minGasPrice,
        gasLimit: 23_000,
    })
    let response = await helpers.callRPCMethod(
        'eth_sendRawTransaction',
        [signedTx.rawTransaction]
    )
    assert.equal(200, response.status)
    assert.isDefined(response.body)

    assert.equal(
        response.body.error.message,
        'insufficient funds for gas * price + value: balance 10000000, tx cost 13550000, overshot 3550000'
    )
})

it('should fail when insufficient balance for transfer + gas', async () => {
    let receiver = web3.eth.accounts.create()

    await helpers.signAndSend({
        from: conf.eoa.address,
        to: receiver.address,
        value: 10_000_000,
        gasPrice: conf.minGasPrice,
        gasLimit: 55_000,
    })

    let signedTx = await receiver.signTransaction({
        from: receiver.address,
        to: conf.eoa.address,
        value: 7_000_000,
        gasPrice: conf.minGasPrice,
        gasLimit: 23_000,
    })
    let response = await helpers.callRPCMethod(
        'eth_sendRawTransaction',
        [signedTx.rawTransaction]
    )
    assert.equal(200, response.status)
    assert.isDefined(response.body)

    assert.equal(
        response.body.error.message,
        'insufficient funds for gas * price + value: balance 10000000, tx cost 10450000, overshot 450000'
    )
})
