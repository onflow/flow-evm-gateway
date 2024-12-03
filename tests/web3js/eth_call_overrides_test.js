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

it('should apply block overrides', async () => {
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

    // Override the `block.number` value to `0x674DB1E1`.
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
    response = await helpers.callRPCMethod(
        'eth_call',
        [call, 'latest', null, { random: '0x7914bb5b13bac6f621bc37bbf6e406fbf4472aaaaf17ec2f309a92aca4e27fc0' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.equal(
        web3.utils.hexToNumber(response.body.result),
        54766484701870566448245359081326515063439899359799766042879591799681605337024n
    )
    assert.notEqual(web3.utils.hexToNumber(response.body.result), currentPrevRandao)
})
