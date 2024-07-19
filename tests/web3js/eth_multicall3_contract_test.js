const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('deploy contract and interact', async () => {
    let deployed = await helpers.deployContract("storage")
    let contractAddress = deployed.receipt.contractAddress

    // get the default deployed value on contract
    const initValue = 1337
    let callRetrieve = await deployed.contract.methods.retrieve().encodeABI()
    result = await web3.eth.call({ to: contractAddress, data: callRetrieve }, "latest")
    assert.equal(result, initValue)

    let multicall3 = await helpers.deployContract("multicall3")
    let multicall3Address = multicall3.receipt.contractAddress

    // make sure deploy results are correct
    assert.equal(multicall3.receipt.status, conf.successStatus)
    assert.isString(multicall3.receipt.transactionHash)
    assert.isString(multicall3Address)

    let callSum20 = await deployed.contract.methods.sum(10, 10).encodeABI()
    let callSum50 = await deployed.contract.methods.sum(10, 40).encodeABI()
    let callAggregate3 = await multicall3.contract.methods.aggregate3(
        [
            {
                target: contractAddress,
                allowFailure: false,
                callData: callSum20
            },
            {
                target: contractAddress,
                allowFailure: false,
                callData: callSum50
            }
        ]
    ).encodeABI()

    result = await web3.eth.call(
        {
            to: multicall3Address,
            data: callAggregate3
        },
        "latest"
    )
    let decodedResult = web3.eth.abi.decodeParameter(
        {
            'Result[]': {
                'success': 'bool',
                'returnData': 'bytes'
            }
        },
        result
    )

    assert.lengthOf(decodedResult, 2)

    assert.equal(decodedResult[0].success, true)
    assert.equal(decodedResult[0].returnData, 20n)

    assert.equal(decodedResult[1].success, true)
    assert.equal(decodedResult[1].returnData, 50n)
})
