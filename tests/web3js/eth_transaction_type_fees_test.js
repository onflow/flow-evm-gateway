const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

let deployed = null
let contractAddress = null

before(async () => {
    deployed = await helpers.deployContract("storage")
    contractAddress = deployed.receipt.contractAddress

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
})

it('calculates fees for legacy tx type', async () => {
    let senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, 4999999999949180300n)

    let storeCallData = deployed.contract.methods.store(1337).encodeABI()
    let gasPrice = conf.minGasPrice + 50n
    let res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: storeCallData,
        value: '0',
        gasPrice: gasPrice
    })
    assert.equal(res.receipt.status, conf.successStatus)
    assert.equal(res.receipt.type, 0n)
    assert.equal(res.receipt.effectiveGasPrice, gasPrice)

    // assert the transaction fees charged on sender EOA
    let cost = res.receipt.gasUsed * gasPrice
    let expectedBalance = senderBalance - cost
    senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, expectedBalance)

    // assert the gasPrice field of the submitted tx
    let latest = await web3.eth.getBlockNumber()
    let tx = await web3.eth.getTransactionFromBlock(latest, 0)
    assert.equal(tx.gasPrice, gasPrice)

    // with insufficient gas price
    try {
        res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: storeCallData,
            value: '0',
            gasPrice: conf.minGasPrice - 50n
        })
    } catch (e) {
        assert.include(e.message, "the minimum accepted gas price for transactions is: 150")
    }

    let coinbaseBalance = await web3.eth.getBalance(conf.serviceEOA)
    assert.equal(coinbaseBalance, 55585700n)
})

it('calculates fees for access list tx type', async () => {
    let senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, 4999999999944414300n)

    let storeCallData = deployed.contract.methods.store(8250).encodeABI()
    let gasPrice = conf.minGasPrice + 5n
    let res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: storeCallData,
        value: '0',
        gasPrice: gasPrice,
        accessList: [
            {
                address: contractAddress,
                storageKeys: [],
            },
        ]
    })
    assert.equal(res.receipt.status, conf.successStatus)
    assert.equal(res.receipt.type, 1n)
    assert.equal(res.receipt.effectiveGasPrice, gasPrice)

    // assert the transaction fees charged on sender EOA
    let cost = res.receipt.gasUsed * gasPrice
    let expectedBalance = senderBalance - cost
    senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, expectedBalance)

    // assert the gasPrice field of the submitted tx
    let latest = await web3.eth.getBlockNumber()
    let tx = await web3.eth.getTransactionFromBlock(latest, 0)
    assert.equal(tx.gasPrice, gasPrice)

    // with insufficient gas price
    try {
        res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: storeCallData,
            value: '0',
            gasPrice: conf.minGasPrice - 50n,
            accessList: [
                {
                    address: contractAddress,
                    storageKeys: [],
                },
            ]
        })
    } catch (e) {
        assert.include(e.message, "the minimum accepted gas price for transactions is: 150")
    }

    let coinbaseBalance = await web3.eth.getBalance(conf.serviceEOA)
    assert.equal(coinbaseBalance, 60085350n)
})

it('calculates fees for dynamic fees tx type', async () => {
    let senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, 4999999999939914650n)

    // gasTipCap is less than gasFeeCap
    // price = Min(GasTipCap, GasFeeCap) when baseFee = 0
    let storeCallData = deployed.contract.methods.store(10007).encodeABI()
    let gasTipCap = conf.minGasPrice + 20n
    let gasFeeCap = conf.minGasPrice + 50n
    let res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: storeCallData,
        value: '0',
        maxPriorityFeePerGas: gasTipCap,
        maxFeePerGas: gasFeeCap,
    })
    assert.equal(res.receipt.status, conf.successStatus)
    assert.equal(res.receipt.type, 2n)
    assert.equal(res.receipt.effectiveGasPrice, gasTipCap)

    // assert the transaction fees charged on sender EOA
    let cost = res.receipt.gasUsed * gasTipCap
    let expectedBalance = senderBalance - cost
    senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, expectedBalance)

    // assert the gas price related fields of the submitted tx
    let latest = await web3.eth.getBlockNumber()
    let tx = await web3.eth.getTransactionFromBlock(latest, 0)
    assert.equal(tx.maxPriorityFeePerGas, gasTipCap)
    assert.equal(tx.maxFeePerGas, gasFeeCap)
    assert.equal(tx.gasPrice, gasFeeCap)

    // gasTipCap is equal to gasFeeCap
    // price = Min(GasTipCap, GasFeeCap) when baseFee = 0
    gasTipCap = conf.minGasPrice + 30n
    gasFeeCap = conf.minGasPrice + 30n
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: storeCallData,
        value: '0',
        maxPriorityFeePerGas: gasTipCap,
        maxFeePerGas: gasFeeCap,
    })
    assert.equal(res.receipt.status, conf.successStatus)
    assert.equal(res.receipt.type, 2n)
    assert.equal(res.receipt.effectiveGasPrice, gasFeeCap)

    // assert the transaction fees charged on sender EOA
    cost = res.receipt.gasUsed * gasFeeCap
    expectedBalance = senderBalance - cost
    senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, expectedBalance)

    // assert the gas price related fields of the submitted tx
    latest = await web3.eth.getBlockNumber()
    tx = await web3.eth.getTransactionFromBlock(latest, 0)
    assert.equal(tx.maxPriorityFeePerGas, gasTipCap)
    assert.equal(tx.maxFeePerGas, gasFeeCap)
    assert.equal(tx.gasPrice, gasFeeCap)

    // gasTipCap cannot be greater than gasFeeCap, so we have covered
    // all cases above.

    // when GasTipCap(a.k.a maxPriorityFeePerGas) is below the required gas price
    try {
        res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: storeCallData,
            value: '0',
            maxPriorityFeePerGas: 50n,
            maxFeePerGas: conf.minGasPrice + 50n,
        })
    } catch (e) {
        assert.include(e.message, "the minimum accepted gas price for transactions is: 150")
    }

    // when GasFeeCap(a.k.a maxFeePerGas) is below the required gas price
    try {
        res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: storeCallData,
            value: '0',
            maxPriorityFeePerGas: conf.minGasPrice - 10n,
            maxFeePerGas: conf.minGasPrice - 10n,
        })
    } catch (e) {
        assert.include(e.message, "the minimum accepted gas price for transactions is: 150")
    }

    let coinbaseBalance = await web3.eth.getBalance(conf.serviceEOA)
    assert.equal(coinbaseBalance, 70093350n)
})
