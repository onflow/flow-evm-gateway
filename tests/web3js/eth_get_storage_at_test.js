const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('can get storage at position', async () => {
    let deployed = await helpers.deployContract("storage")
    let contractAddress = deployed.receipt.contractAddress

    // make sure deploy results are correct
    assert.equal(deployed.receipt.status, conf.successStatus)
    assert.isString(deployed.receipt.transactionHash)
    assert.isString(contractAddress)
    assert.equal(deployed.receipt.from, conf.eoa.address)
    assert.isUndefined(deployed.receipt.to)

    let rcp = await web3.eth.getTransactionReceipt(deployed.receipt.transactionHash)
    assert.equal(rcp.contractAddress, contractAddress)
    assert.equal(rcp.status, conf.successStatus)
    assert.isUndefined(rcp.to)

    // check if latest block contains the deploy results
    let latestHeight = await web3.eth.getBlockNumber()
    let deployTx = await web3.eth.getTransactionFromBlock(latestHeight, 0)
    assert.equal(deployTx.hash, deployed.receipt.transactionHash)
    assert.isUndefined(deployTx.to)

    let result = await web3.eth.getStorageAt(contractAddress, 0)
    assert.equal(result, 1337n)

    // update the value on the contract
    let newValue = 100
    let updateData = deployed.contract.methods.store(newValue).encodeABI()
    // store a value in the contract
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: updateData,
        value: '0',
        gasPrice: '0',
    })
    assert.equal(res.receipt.status, conf.successStatus)

    result = await web3.eth.getStorageAt(contractAddress, 0)
    assert.equal(result, newValue)

    let key = web3.utils.padLeft(conf.eoa.address, 64) + web3.utils.padLeft('1', 64)
    result = await web3.eth.getStorageAt(contractAddress, web3.utils.sha3(key, { "encoding": "hex" }))
    assert.equal(result, 100n)

    result = await web3.eth.getStorageAt(contractAddress, '0x15')
    assert.equal(result, 0n)
})
