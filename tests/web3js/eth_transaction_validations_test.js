const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')

it('validates transactions', async () => {
    // Valid
    let txArgs = {
        from: "0x000000000000000000000000000000000000dead",
        to: "0x000000000000000000000000000000000000dEaD",
        gas: 25_000,
        gasPrice: 0,
        value: 0
    }
    let signedTx = await conf.eoa.signTransaction(txArgs)
    let response = await helpers.callRPCMethod('eth_sendRawTransaction', [signedTx.rawTransaction])
    assert.equal(200, response.status)
    let result = response.body['result']
    assert.isDefined(result)
    assert.isUndefined(response.body['error'])

    // Send to 0 address
    txArgs = {
        from: "0x000000000000000000000000000000000000dead",
        to: "0x0000000000000000000000000000000000000000",
        gas: 25_000,
        gasPrice: 0,
        value: 0
    }
    signedTx = await conf.eoa.signTransaction(txArgs)
    response = await helpers.callRPCMethod('eth_sendRawTransaction', [signedTx.rawTransaction])
    assert.equal(200, response.status)
    result = response.body['error']['message']
    assert.equal('transaction recipient is the zero address', result)

    // Create empty contract (no value)
    txArgs = {
        from: "0x000000000000000000000000000000000000dead",
        to: null,
        gas: 53_000,
        gasPrice: 0,
        value: 0,
        data: ""
    }
    signedTx = await conf.eoa.signTransaction(txArgs)
    response = await helpers.callRPCMethod('eth_sendRawTransaction', [signedTx.rawTransaction])
    assert.equal(200, response.status)
    result = response.body['error']['message']
    assert.equal('transaction will create a contract with empty code', result)

    // Create empty contract (with value)
    txArgs = {
        from: "0x000000000000000000000000000000000000dead",
        to: null,
        gas: 53_000,
        gasPrice: 0,
        value: 150
    }
    signedTx = await conf.eoa.signTransaction(txArgs)
    response = await helpers.callRPCMethod('eth_sendRawTransaction', [signedTx.rawTransaction])
    assert.equal(200, response.status)
    result = response.body['error']['message']
    assert.equal('transaction will create a contract with value but empty code', result)

    // Small payload for create
    txArgs = {
        from: "0x000000000000000000000000000000000000dead",
        to: null,
        gas: 153_000,
        gasPrice: 0,
        value: 150,
        data: "0xc6888fa1"
    }
    signedTx = await conf.eoa.signTransaction(txArgs)
    response = await helpers.callRPCMethod('eth_sendRawTransaction', [signedTx.rawTransaction])
    assert.equal(200, response.status)
    result = response.body['error']['message']
    assert.equal('transaction will create a contract, but the payload is suspiciously small (4 bytes)', result)

    // tx data length < 4
    txArgs = {
        from: "0x000000000000000000000000000000000000dead",
        to: "0x000000000000000000000000000000000000dead",
        gas: 153_000,
        gasPrice: 0,
        value: 150,
        data: "0xc6888f"
    }
    signedTx = await conf.eoa.signTransaction(txArgs)
    response = await helpers.callRPCMethod('eth_sendRawTransaction', [signedTx.rawTransaction])
    assert.equal(200, response.status)
    result = response.body['error']['message']
    assert.equal('transaction data is not valid ABI (missing the 4 byte call prefix)', result)

    // tx data (excluding function selector) not divisible by 32
    txArgs = {
        from: "0x000000000000000000000000000000000000dead",
        to: "0x000000000000000000000000000000000000dead",
        gas: 153_000,
        gasPrice: 0,
        value: 150,
        data: "0xc6888fa100000000000000000000000000000000000000000000000000000000000000"
    }
    signedTx = await conf.eoa.signTransaction(txArgs)
    response = await helpers.callRPCMethod('eth_sendRawTransaction', [signedTx.rawTransaction])
    assert.equal(200, response.status)
    result = response.body['error']['message']
    assert.equal('transaction data is not valid ABI (length should be a multiple of 32 (was 31))', result)

}).timeout(10 * 1000)
