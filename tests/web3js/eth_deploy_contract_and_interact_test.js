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

    // check that getCode supports specific block heights
    let code = await web3.eth.getCode(contractAddress, latestHeight - 1n)
    assert.equal(code, "0x") // empty at previous height

    code = await web3.eth.getCode(contractAddress)
    // deploy data has more than just the contract
    // since it contains the initialization code,
    // but subset of the data is the contract code
    assert.isTrue(deployTx.data.includes(code.replace("0x", "")))

    let deployReceipt = await web3.eth.getTransactionReceipt(deployed.receipt.transactionHash)
    assert.deepEqual(deployReceipt, deployed.receipt)

    // get the default deployed value on contract
    const initValue = 1337
    let callRetrieve = await deployed.contract.methods.retrieve().encodeABI()
    result = await web3.eth.call({to: contractAddress, data: callRetrieve}, "latest")
    assert.equal(result, initValue)

    // set the value on the contract, to its current value
    let updateData = deployed.contract.methods.store(initValue).encodeABI()
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
    result = await web3.eth.call({to: contractAddress, data: callRetrieve}, "latest")
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
        gasPrice: '0',
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // check the new value on contract
    result = await web3.eth.call({to: contractAddress, data: callRetrieve}, "latest")
    assert.equal(result, newValue)

    // make sure receipts and txs are indexed
    latestHeight = await web3.eth.getBlockNumber()
    let updateTx = await web3.eth.getTransactionFromBlock(latestHeight, 0)
    let updateRcp = await web3.eth.getTransactionReceipt(updateTx.hash)
    assert.equal(updateRcp.status, conf.successStatus)
    assert.equal(updateTx.data, updateData)

    // check that call can handle specific block heights
    result = await web3.eth.call({to: contractAddress, data: callRetrieve}, latestHeight - 1n)
    assert.equal(result, initValue)

    // submit a transaction that emits logs
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: deployed.contract.methods.sum(100, 200).encodeABI(),
        gas: 1000000,
        gasPrice: 0
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // assert that logsBloom from transaction receipt and block match
    latestHeight = await web3.eth.getBlockNumber()
    let block = await web3.eth.getBlock(latestHeight)
    assert.equal(block.logsBloom, res.receipt.logsBloom)

}).timeout(10*1000)
