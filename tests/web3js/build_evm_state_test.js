const utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('should handle a large number of EVM interactions', async () => {
    let latest = await web3.eth.getBlockNumber()
    assert.equal(latest, 2n)

    let eoaCount = 20
    let accounts = []

    // Generate 20 EOAs
    // Fund them with some arbitrary number of tokens
    // Make them do transfers to each other

    for (let i = 0; i < eoaCount; i++) {
        let receiver = web3.eth.accounts.create()

        let transferValue = utils.toWei('0.15', 'ether')
        let transfer = await helpers.signAndSend({
            from: conf.eoa.address,
            to: receiver.address,
            value: transferValue,
            gasPrice: conf.minGasPrice,
            gasLimit: 21_000,
        })

        assert.equal(transfer.receipt.status, conf.successStatus)
        assert.equal(transfer.receipt.from, conf.eoa.address)
        assert.equal(transfer.receipt.to, receiver.address)

        // check balance was moved
        let receiverWei = await web3.eth.getBalance(receiver.address)
        assert.equal(receiverWei, transferValue)

        accounts.push(receiver)
    }

    let senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, 1999999999937000000n)

    // Each EOA has a 0.15 ether, so the below transfer amounts
    // should never add up to that, or the transfer transaction
    // will revert.
    let transferAmounts = ['0.01', '0.02', '0.04']
    for (let i = 0; i < eoaCount; i++) {
        let sender = accounts[i]

        for (let j = 0; j < 3; j++) {
            let receiver = randomItem(accounts)

            let amount = randomItem(transferAmounts)
            let transferValue = utils.toWei(amount, 'ether')
            let transfer = await helpers.signAndSendFrom(sender, {
                from: sender.address,
                to: receiver.address,
                value: transferValue,
                gasPrice: conf.minGasPrice,
                gasLimit: 21_000,
            })

            assert.equal(transfer.receipt.status, conf.successStatus)
            assert.equal(transfer.receipt.from, sender.address)
            assert.equal(transfer.receipt.to, receiver.address)
        }
    }

    latest = await web3.eth.getBlockNumber()
    assert.equal(latest, 82n)

    let contractAddress = null
    let deployed = null
    for (let i = 0; i < eoaCount; i++) {
        let sender = accounts[i]

        deployed = await helpers.deployContractFrom(sender, 'storage')
        contractAddress = deployed.receipt.contractAddress

        assert.equal(deployed.receipt.status, conf.successStatus)
        assert.isString(contractAddress)
        assert.equal(deployed.receipt.from, sender.address)

        let storeNumber = Math.floor(Math.random() * 10_000)
        // set the value on the contract, to its current value
        let updateData = deployed.contract.methods.store(storeNumber).encodeABI()
        // store a value in the contract
        let res = await helpers.signAndSendFrom(sender, {
            from: sender.address,
            to: contractAddress,
            data: updateData,
            value: '0',
            gasPrice: conf.minGasPrice,
        })
        assert.equal(res.receipt.status, conf.successStatus)

        sender = randomItem(accounts)
        let sumA = Math.floor(Math.random() * 10_000)
        let sumB = Math.floor(Math.random() * 100_000)
        res = await helpers.signAndSendFrom(sender, {
            from: sender.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(sumA, sumB).encodeABI(),
            gas: 55_000,
            gasPrice: conf.minGasPrice
        })
        assert.equal(res.receipt.status, conf.successStatus)
    }

    latest = await web3.eth.getBlockNumber()
    assert.equal(latest, 142n)

    // submit a transaction that calls blockNumber()
    let blockNumberData = deployed.contract.methods.blockNumber().encodeABI()
    let res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: blockNumberData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // submit a transaction that calls blockTime()
    let blockTimeData = deployed.contract.methods.blockNumber().encodeABI()
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: blockTimeData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // submit a transaction that calls blockHash(uint num)
    let blockHashData = deployed.contract.methods.blockHash(110).encodeABI()
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: blockHashData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // submit a transaction that calls random()
    let randomData = deployed.contract.methods.random().encodeABI()
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: randomData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // submit a transaction that calls chainID()
    let chainIDData = deployed.contract.methods.chainID().encodeABI()
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: chainIDData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // submit a transaction that calls verifyArchCallToRandomSource(uint64 height)
    let getRandomSourceData = deployed.contract.methods.verifyArchCallToRandomSource(120).encodeABI()
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: getRandomSourceData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // submit a transaction that calls verifyArchCallToRevertibleRandom()
    let revertibleRandomData = deployed.contract.methods.verifyArchCallToRevertibleRandom().encodeABI()
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: revertibleRandomData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // submit a transaction that calls verifyArchCallToFlowBlockHeight()
    let flowBlockHeightData = deployed.contract.methods.verifyArchCallToFlowBlockHeight().encodeABI()
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: flowBlockHeightData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // submit a transaction that calls verifyArchCallToVerifyCOAOwnershipProof(address,bytes32,bytes)
    let tx = await web3.eth.getTransactionFromBlock(conf.startBlockHeight, 1)
    let verifyCOAOwnershipProofData = deployed.contract.methods.verifyArchCallToVerifyCOAOwnershipProof(
        tx.to,
        '0x1bacdb569847f31ade07e83d6bb7cefba2b9290b35d5c2964663215e73519cff',
        web3.utils.hexToBytes('f853c18088f8d6e0586b0a20c78365766df842b840b90448f4591df2639873be2914c5560149318b7e2fcf160f7bb8ed13cfd97be2f54e6889606f18e50b2c37308386f840e03a9fff915f57b2164cba27f0206a95')
    ).encodeABI()
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: verifyCOAOwnershipProofData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)
})

function randomItem(items) {
    return items[Math.floor(Math.random() * items.length)]
}
