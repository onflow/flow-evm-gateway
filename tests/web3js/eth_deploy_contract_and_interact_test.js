const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('deploy contract and interact', async () => {
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
    assert.equal(rcp.gasUsed, 338798n)
    assert.equal(rcp.gasUsed, rcp.cumulativeGasUsed)

    // check if latest block contains the deploy results
    let latestHeight = await web3.eth.getBlockNumber()
    console.log("LatestHeight: ", latestHeight)
    let deployTx = await web3.eth.getTransactionFromBlock(latestHeight, 0)
    console.log("DeployTX: ", deployTx)
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
    result = await web3.eth.call({ to: contractAddress, data: callRetrieve }, "latest")
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
    result = await web3.eth.call({ to: contractAddress, data: callRetrieve }, "latest")
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
    result = await web3.eth.call({ to: contractAddress, data: callRetrieve }, "latest")
    assert.equal(result, newValue)

    // make sure receipts and txs are indexed
    latestHeight = await web3.eth.getBlockNumber()
    let updateTx = await web3.eth.getTransactionFromBlock(latestHeight, 0)
    let updateRcp = await web3.eth.getTransactionReceipt(updateTx.hash)
    assert.equal(updateRcp.status, conf.successStatus)
    assert.equal(updateTx.data, updateData)

    // check that call can handle specific block heights
    result = await web3.eth.call({ to: contractAddress, data: callRetrieve }, latestHeight - 1n)
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

    // check that revert reason for custom error is correctly returned for signed transaction
    try {
        let callCustomError = deployed.contract.methods.customError().encodeABI()
        result = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: callCustomError,
            gas: 1_000_000,
            gasPrice: 0
        })
    } catch (error) {
        assert.equal(error.reason, 'execution reverted')
        assert.equal(error.signature, '0x9195785a')
        assert.equal(
            error.data,
            '00000000000000000000000000000000000000000000000000000000000000050000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000001056616c756520697320746f6f206c6f7700000000000000000000000000000000'
        )
    }

    // check that revert reason for custom error is correctly returned for contract call
    // and it is properly ABI decoded.
    try {
        result = await deployed.contract.methods.customError().call({ from: conf.eoa.address })
    } catch (err) {
        let error = err.innerError
        assert.equal(
            error.data,
            '0x9195785a00000000000000000000000000000000000000000000000000000000000000050000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000001056616c756520697320746f6f206c6f7700000000000000000000000000000000'
        )
        assert.equal(error.errorName, 'MyCustomError')
        assert.equal(error.errorSignature, 'MyCustomError(uint256,string)')
        assert.equal(error.errorArgs.value, 5n)
        assert.equal(error.errorArgs.message, 'Value is too low')
    }

    // check that assertion error is correctly returned for signed transaction
    try {
        let callAssertError = deployed.contract.methods.assertError().encodeABI()
        result = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: callAssertError,
            gas: 1_000_000,
            gasPrice: 0
        })
    } catch (error) {
        assert.equal(error.reason, 'execution reverted: Assert Error Message')
        assert.equal(error.signature, '0x08c379a0')
        assert.equal(
            error.data,
            '00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000014417373657274204572726f72204d657373616765000000000000000000000000'
        )
    }

    // check that assertion error is correctly returned for contract call
    // and it is properly ABI decoded.
    try {
        result = await deployed.contract.methods.assertError().call({ from: conf.eoa.address })
    } catch (err) {
        let error = err.innerError
        assert.equal(
            error.message,
            'execution reverted: Assert Error Message'
        )
        assert.equal(
            error.data,
            '0x08c379a000000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000014417373657274204572726f72204d657373616765000000000000000000000000'
        )
    }

    // check that revert reason for custom error is correctly returned for gas estimation
    try {
        let callCustomError = deployed.contract.methods.customError().encodeABI()
        result = await web3.eth.estimateGas({
            from: conf.eoa.address,
            to: contractAddress,
            data: callCustomError,
            gas: 1_000_000,
            gasPrice: 0
        })
    } catch (error) {
        assert.equal(error.innerError.message, 'execution reverted')
        assert.equal(
            error.innerError.data,
            '0x9195785a00000000000000000000000000000000000000000000000000000000000000050000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000001056616c756520697320746f6f206c6f7700000000000000000000000000000000'
        )
    }

    // check that assertion error is correctly returned for gas estimation
    try {
        let callAssertError = deployed.contract.methods.assertError().encodeABI()
        result = await web3.eth.estimateGas({
            from: conf.eoa.address,
            to: contractAddress,
            data: callAssertError,
            gas: 1_000_000,
            gasPrice: 0
        })
    } catch (error) {
        assert.equal(error.innerError.message, 'execution reverted: Assert Error Message')
        assert.equal(
            error.innerError.data,
            '0x08c379a000000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000014417373657274204572726f72204d657373616765000000000000000000000000'
        )
    }

})
