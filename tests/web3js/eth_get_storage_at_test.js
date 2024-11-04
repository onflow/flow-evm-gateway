const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('should retrieve storage slots of contracts', async () => {
    let deployed = await helpers.deployContract('storage')
    let contractAddress = deployed.receipt.contractAddress

    // make sure deploy results are correct
    assert.equal(deployed.receipt.status, conf.successStatus)

    // get the default deployed value on contract
    let callRetrieve = await deployed.contract.methods.retrieve().encodeABI()
    let result = await web3.eth.call({ to: contractAddress, data: callRetrieve }, 'latest')

    let slot = 0 // The slot for the 'number' variable
    let stored = await web3.eth.getStorageAt(contractAddress, slot, 'latest')
    assert.equal(stored, result)

    // set the value on the contract, to its current value
    let initValue = 1337
    let updateData = deployed.contract.methods.store(initValue).encodeABI()
    // store a value in the contract
    let res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: updateData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // check the new value on contract
    result = await web3.eth.call({ to: contractAddress, data: callRetrieve }, 'latest')
    assert.equal(result, initValue)

    // update the value on the contract
    newValue = 100
    updateData = deployed.contract.methods.store(newValue).encodeABI()
    // store a value in the contract
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: updateData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    let latestHeight = await web3.eth.getBlockNumber()

    // assert the storage slot on latest block
    stored = await web3.eth.getStorageAt(contractAddress, slot, latestHeight)
    value = web3.eth.abi.decodeParameter('uint256', stored)
    assert.equal(value, 100n)

    // // assert the storage slot on previous block
    stored = await web3.eth.getStorageAt(contractAddress, slot, latestHeight - 1n)
    value = web3.eth.abi.decodeParameter('uint256', stored)
    assert.equal(value, 1337n)

    // assert the storage slot on block of contract deployment
    stored = await web3.eth.getStorageAt(contractAddress, slot, deployed.receipt.blockNumber)
    value = web3.eth.abi.decodeParameter('uint256', stored)
    assert.equal(value, 1337n)

    // assert the storage slot on block prior to contract deployment
    stored = await web3.eth.getStorageAt(contractAddress, slot, deployed.receipt.blockNumber - 1n)
    value = web3.eth.abi.decodeParameter('uint256', stored)
    assert.equal(value, 0n)
})
