const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

let deployed = null
let contractAddress = null

before(async () => {
    deployed = await helpers.deployContract('blockOverrides')
    contractAddress = deployed.receipt.contractAddress

    assert.equal(deployed.receipt.status, conf.successStatus)
})

it('should apply block overrides on eth_estimateGas', async () => {
    assert.equal(deployed.receipt.status, conf.successStatus)

    let receipt = await web3.eth.getTransactionReceipt(deployed.receipt.transactionHash)
    assert.equal(receipt.contractAddress, contractAddress)

    // Check the `block.number` value, without overrides
    let testFuncSelector = deployed.contract.methods.test().encodeABI()
    let txArgs = {
        from: conf.eoa.address,
        to: contractAddress,
        gas: '0x493E0',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
        value: '0x0',
        data: testFuncSelector,
    }

    let response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 21473n)

    // Override the `block.number` value to `9090`.
    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, { number: '0x2382' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 273693n)

    // Check the `block.timestamp` value, without overrides
    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 21473n)

    // Override the `block.timestamp` value to `0x674DB1E1`.
    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, { time: '0x674DB1E1' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 273693n)

    // Check the `block.prevrandao` value, without overrides
    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 21473n)

    // Override the `block.prevrandao` value to `0x7914bb5b13bac6f621bc37bbf6e406fbf4472aaaaf17ec2f309a92aca4e27fc0`.
    let random = '0x7914bb5b13bac6f621bc37bbf6e406fbf4472aaaaf17ec2f309a92aca4e27fc0'
    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, { prevRandao: random }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 273693n)

    // Check the `block.coinbase` value, without overrides
    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 21473n)

    // Override the `block.coinbase` value to `0x658Bdf435d810C91414eC09147DAA6DB62406379`.
    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, { feeRecipient: '0x658Bdf435d810C91414eC09147DAA6DB62406379' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 273693n)

    // test that gas estimation still allows gas limits above the configured
    // EIP-7825 value
    txArgs = {
        from: conf.eoa.address,
        to: contractAddress,
        gas: '0x10F4240', // this is equal to 17,777,216, which is greater than 16,777,216
        gasPrice: web3.utils.toHex(conf.minGasPrice),
        value: '0x0',
        data: testFuncSelector,
    }

    response = await helpers.callRPCMethod(
        'eth_estimateGas',
        [txArgs, 'latest', null, null]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(web3.utils.hexToNumber(response.body.result), 21473n)
})
