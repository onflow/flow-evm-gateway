const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('returns proper response for eth_getTransactionByHash', async () => {
    let deployed = await helpers.deployContract('storage')
    let contractAddress = deployed.receipt.contractAddress

    // make sure deploy results are correct
    assert.equal(deployed.receipt.status, conf.successStatus)

    let updateData = deployed.contract.methods.store(1337).encodeABI()
    let result = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: updateData,
        gas: 25000,
        value: 0,
        maxFeePerGas: 3000000000,
        maxPriorityFeePerGas: 800000000,
    })
    assert.equal(result.receipt.status, conf.successStatus)

    let tx = await web3.eth.getTransactionFromBlock(result.receipt.blockNumber, 0)
    assert.equal(tx.type, 2n)
    assert.deepEqual(tx.accessList, [])
    assert.equal(tx.chainId, 646n)
    assert.equal(tx.gas, 25000n)
    assert.equal(tx.maxFeePerGas, 3000000000n)
    assert.equal(tx.maxPriorityFeePerGas, 800000000n)
    assert.equal(tx.gasPrice, 3000000000n)
})
