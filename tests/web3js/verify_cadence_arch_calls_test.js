const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('should be able to use Cadence Arch calls', async () => {
    let latest = await web3.eth.getBlockNumber()
    let expectedBlockHeight = conf.startBlockHeight
    assert.equal(latest, expectedBlockHeight)

    let deployed = await helpers.deployContract('storage')
    let contractAddress = deployed.receipt.contractAddress

    // submit a transaction that calls verifyArchCallToRandomSource(uint64 height)
    let getRandomSourceData = deployed.contract.methods.verifyArchCallToRandomSource(2).encodeABI()
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: getRandomSourceData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    // make a contract call for verifyArchCallToRandomSource(uint64 height)
    res = await web3.eth.call({ to: contractAddress, data: getRandomSourceData }, 'latest')
    assert.notEqual(
        res,
        '0x0000000000000000000000000000000000000000000000000000000000000000'
    )
    assert.lengthOf(res, 66)

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

    // make a contract call for verifyArchCallToRevertibleRandom()
    res = await web3.eth.call({ to: contractAddress, data: revertibleRandomData }, 'latest')
    assert.notEqual(
        res,
        '0x0000000000000000000000000000000000000000000000000000000000000000'
    )
    assert.lengthOf(res, 66)

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

    // make a contract call for verifyArchCallToFlowBlockHeight()
    res = await web3.eth.call({ to: contractAddress, data: flowBlockHeightData }, 'latest')
    assert.equal(
        web3.eth.abi.decodeParameter('uint64', res),
        conf.startBlockHeight + 4n,
    )

    // submit a transaction that calls verifyArchCallToVerifyCOAOwnershipProof(address,bytes32,bytes)
    let tx = await web3.eth.getTransactionFromBlock(conf.coaDeploymentHeight, 1)
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

    // make a contract call for verifyArchCallToVerifyCOAOwnershipProof(address,bytes32,bytes)
    res = await web3.eth.call({ to: contractAddress, data: verifyCOAOwnershipProofData }, 'latest')
    assert.equal(
        web3.eth.abi.decodeParameter('bool', res),
        false,
    )
})
