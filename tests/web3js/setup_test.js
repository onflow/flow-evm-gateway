const { assert } = require('chai')
const conf = require('./config')

it('checks test setup', async() => {
    assert.equal(1, 1) // always true
    assert.isNotEmpty(conf.web3)
})
