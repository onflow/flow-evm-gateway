const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

let deployed = null
let contractAddress = null

before(async () => {
    deployed = await helpers.deployContract('storage')
    contractAddress = deployed.receipt.contractAddress

    assert.equal(deployed.receipt.status, conf.successStatus)
})

it('should apply block overrides on eth_estimateGas', async () => {
    assert.equal(deployed.receipt.status, conf.successStatus)

    let receipt = await web3.eth.getTransactionReceipt(deployed.receipt.transactionHash)
    assert.equal(receipt.contractAddress, contractAddress)

    // Check the `block.number` value, without overrides
    let blockNumberSelector = deployed.contract.methods.blockNumber().encodeABI()
    let txArgs = {
        from: conf.eoa.address,
        to: contractAddress,
        gas: '0x75ab',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
        value: '0x0',
        data: blockNumberSelector,
    }

    let response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 21651n)

    // Override the `block.number` value to `2`.
    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, { number: '0x2' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 21651n)

    // Check the `block.timestamp` value, without overrides
    let blockTimeSelector = deployed.contract.methods.blockTime().encodeABI()
    txArgs.data = blockTimeSelector

    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 21607n)

    // Override the `block.timestamp` value to `0x674DB1E1`.
    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, { time: '0x674DB1E1' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 21607n)

    // Check the `block.prevrandao` value, without overrides
    let randomSelector = deployed.contract.methods.random().encodeABI()
    txArgs.data = randomSelector

    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    // Override the `block.prevrandao` value to `0x7914bb5b13bac6f621bc37bbf6e406fbf4472aaaaf17ec2f309a92aca4e27fc0`.
    let random = '0x7914bb5b13bac6f621bc37bbf6e406fbf4472aaaaf17ec2f309a92aca4e27fc0'
    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, { random: random }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 21584n)
})
