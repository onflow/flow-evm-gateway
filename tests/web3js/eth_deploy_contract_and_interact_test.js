const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('deploy contract and interact', async() => {
    let deployed = await helpers.deployContract("storage")
    let contractAddress = deployed.receipt.contractAddress

    // make sure deploy results are correct
    assert.equal(deployed.receipt.status, conf.successStatus)
    assert.isString(deployed.receipt.transactionHash)
    assert.isString(contractAddress)
    assert.equal(deployed.receipt.from, conf.eoa.address)

    let rcp = await web3.eth.getTransactionReceipt(deployed.receipt.transactionHash)
    assert.equal(rcp.contractAddress, contractAddress)
    assert.equal(rcp.status, conf.successStatus)

    // check if latest block contains the deploy results
    let latestHeight = await web3.eth.getBlockNumber()
    let deployTx = await web3.eth.getTransactionFromBlock(latestHeight, 0)
    assert.equal(deployTx.hash, deployed.receipt.transactionHash)

    let code = await web3.eth.getCode(contractAddress)
    // deploy data has more than just the contract
    // since it contains the initialization code,
    // but subset of the data is the contract code
    assert.isTrue(deployTx.data.includes(code.replace("0x", "")))

    let deployReceipt = await web3.eth.getTransactionReceipt(deployed.receipt.transactionHash)
    assert.deepEqual(deployReceipt, deployed.receipt)

    // get the default deployed value on contract
    let result = await deployed.contract.methods.retrieve().call()
    const initValue = 1337
    assert.equal(result, initValue)

    // update the value on the contract
    let newValue = 100
    let updateData = deployed.contract.methods.store(newValue).encodeABI()
    // store a value in the contract
    let res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: updateData,
        value: '0',
        gasPrice: '0',
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // check the new value on contract
    result = await deployed.contract.methods.retrieve().call()
    assert.equal(result, newValue)

    // make sure receipts and txs are indexed
    latestHeight = await web3.eth.getBlockNumber()
    let updateTx = await web3.eth.getTransactionFromBlock(latestHeight, 0)
    let updateRcp = await web3.eth.getTransactionReceipt(updateTx.hash)
    assert.equal(updateRcp.status, conf.successStatus)
    assert.equal(updateTx.data, updateData)

}).timeout(10*1000)

