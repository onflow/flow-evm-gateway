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

it('should apply block overrides on eth_call', async () => {
    assert.equal(deployed.receipt.status, conf.successStatus)

    let receipt = await web3.eth.getTransactionReceipt(deployed.receipt.transactionHash)
    assert.equal(receipt.contractAddress, contractAddress)

    let latestBlockNumber = await web3.eth.getBlockNumber()

    // Check the `block.number` value, without overrides
    let blockNumberSelector = deployed.contract.methods.blockNumber().encodeABI()
    let call = {
        from: conf.eoa.address,
        to: contractAddress,
        gas: '0x75ab',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
        value: '0x0',
        data: blockNumberSelector,
    }

    let response = await helpers.callRPCMethod(
        'eth_call',
        [call, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), latestBlockNumber)

    // Override the `block.number` value to `2`.
    response = await helpers.callRPCMethod(
        'eth_call',
        [call, 'latest', null, { number: '0x2' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 2n)

    // Check the `block.timestamp` value, without overrides
    let block = await web3.eth.getBlock(latestBlockNumber)
    let blockTimeSelector = deployed.contract.methods.blockTime().encodeABI()
    call.data = blockTimeSelector

    response = await helpers.callRPCMethod(
        'eth_call',
        [call, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), block.timestamp)

    // Override the `block.timestamp` value to `0x674DB1E1`.
    response = await helpers.callRPCMethod(
        'eth_call',
        [call, 'latest', null, { time: '0x674DB1E1' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 1733145057n)

    // Check the `block.prevrandao` value, without overrides
    let randomSelector = deployed.contract.methods.random().encodeABI()
    call.data = randomSelector

    response = await helpers.callRPCMethod(
        'eth_call',
        [call, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    let currentPrevRandao = web3.utils.hexToNumber(response.body.result)

    // Override the `block.prevrandao` value to `0x7914bb5b13bac6f621bc37bbf6e406fbf4472aaaaf17ec2f309a92aca4e27fc0`.
    let random = '0x7914bb5b13bac6f621bc37bbf6e406fbf4472aaaaf17ec2f309a92aca4e27fc0'
    response = await helpers.callRPCMethod(
        'eth_call',
        [call, 'latest', null, { prevRandao: random }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(response.body.result, random)
    assert.notEqual(web3.utils.hexToNumber(response.body.result), currentPrevRandao)
})

it('should apply block overrides on debug_traceCall', async () => {
    assert.equal(deployed.receipt.status, conf.successStatus)

    let receipt = await web3.eth.getTransactionReceipt(deployed.receipt.transactionHash)
    assert.equal(receipt.contractAddress, contractAddress)

    let callTracer = {
        tracer: 'callTracer',
        tracerConfig: {
            withLog: false,
            onlyTopCall: true
        }
    }

    let latestBlockNumber = await web3.eth.getBlockNumber()

    // Check the `block.number` value, without overrides
    let blockNumberSelector = deployed.contract.methods.blockNumber().encodeABI()
    let call = {
        from: conf.eoa.address,
        to: contractAddress,
        gas: '0x75ab',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
        value: '0x0',
        data: blockNumberSelector,
    }

    let response = await helpers.callRPCMethod(
        'debug_traceCall',
        [call, 'latest', callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result.output), latestBlockNumber)

    // Override the `block.number` value to `2`.
    callTracer.blockOverrides = { number: '0x2' }
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [call, 'latest', callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result.output), 2n)

    // Check the `block.timestamp` value, without overrides
    let block = await web3.eth.getBlock(latestBlockNumber)
    let blockTimeSelector = deployed.contract.methods.blockTime().encodeABI()
    call.data = blockTimeSelector

    callTracer.blockOverrides = {}
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [call, 'latest', callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result.output), block.timestamp)

    // Override the `block.timestamp` value to `0x674DB1E1`.
    callTracer.blockOverrides = { time: '0x674DB1E1' }
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [call, 'latest', callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result.output), 1733145057n)

    // Check the `block.prevrandao` value, without overrides
    let randomSelector = deployed.contract.methods.random().encodeABI()
    call.data = randomSelector

    callTracer.blockOverrides = {}
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [call, 'latest', callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    let currentPrevRandao = web3.utils.hexToNumber(response.body.result.output)

    // Override the `block.prevrandao` value to `0x7914bb5b13bac6f621bc37bbf6e406fbf4472aaaaf17ec2f309a92aca4e27fc0`.
    let random = '0x7914bb5b13bac6f621bc37bbf6e406fbf4472aaaaf17ec2f309a92aca4e27fc0'
    callTracer.blockOverrides = { prevRandao: random }
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [call, 'latest', callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(response.body.result.output, random)
    assert.notEqual(web3.utils.hexToNumber(response.body.result.output), currentPrevRandao)
})
