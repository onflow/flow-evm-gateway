const { assert } = require('chai')
const helpers = require('./helpers')

it('should retrieve flow height', async () => {
    response = await helpers.callRPCMethod(
        'debug_flowHeight',
        ['latest']
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    let height = response.body.result
    assert.isTrue(height > 0)
})
