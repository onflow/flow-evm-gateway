const { assert } = require('chai')
const helpers = require('./helpers')
const conf = require('./config')

it('should retrieve flow height', async () => {
    let response = await helpers.callRPCMethod(
        'debug_flowHeightByBlock',
        ['latest']
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    let height = response.body.result
    assert.equal(height, conf.startBlockHeight)
})
