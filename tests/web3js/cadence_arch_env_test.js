const utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

describe('calls cadence arch functions and block environment functions', async () => {
    let deployed = await helpers.deployContract('storage')
    let contractAddress = deployed.receipt.contractAddress
    let methods = deployed.contract.methods

    async function testEmitTx(method) {
        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: method.encodeABI(),
            value: '0',
            gasPrice: conf.minGasPrice,
        })
        assert.equal(res.receipt.status, conf.successStatus)
        assert.equal(res.receipt.logs.length, 1)

        return res
    }

    async function testCall(method) {
        let block = await web3.eth.getBlock('latest')

        let value = await web3.eth.call({
            to: contractAddress,
            data: method.encodeABI()
        }, block.number)

        assert.isAbove(value.length, 0)

        return {
            value: value,
            block: block,
        }
    }

    it('calls blockNumber', async () => {
        await testEmitTx(methods.emitBlockNumber())

        let res = await testCall(methods.blockNumber())
        assert.equal(
            web3.eth.abi.decodeParameter('uint256', res.value),
            res.block.number,
        )
    })

    it('calls blockTime', async function() {
        await testEmitTx(methods.emitBlockTime())

        let res = await testCall(methods.blockTime())
        assert.equal(
            web3.eth.abi.decodeParameter('uint', res.value),
            res.block.timestamp,
        )
    })

    it('calls blockHash', async function() {
        await testEmitTx(methods.emitBlockHash())

        let res = await testCall(methods.blockHash())
        assert.equal(
            web3.eth.abi.decodeParameter('bytes32', res.value),
            res.block.hash,
        )
    })

    it('calls random', async function() {
        await testEmitTx(methods.emitRandom())

        let res = await testCall(methods.random())
        assert.equal(
            web3.eth.abi.decodeParameter('uint256', res.value),
            res.block.difficulty
        )
    })

    it('calls chainID', async function() {
        await testEmitTx(methods.emitChainID())
        await testCall(methods.chainID())
    })

    it('calls verifyArchCallToFlowBlockHeight', async function() {
        await testEmitTx(methods.emitVerifyArchCallToFlowBlockHeight())

        let res = await testCall(methods.verifyArchCallToFlowBlockHeight())
        assert.equal(
            web3.eth.abi.decodeParameter('uint64', res.value),
            res.block.number,
        )
    })

    it('calls verifyArchCallToRandomSource', async function() {
        await testEmitTx(methods.emitVerifyArchCallToRandomSource(1))

        let res = await testCall(methods.verifyArchCallToRandomSource(1))
        assert.notEqual(
            res.value,
            '0x0000000000000000000000000000000000000000000000000000000000000000'
        )
        assert.lengthOf(res.value, 66)
    })


    it('calls verifyArchCallToRevertibleRandom', async function() {
        await testEmitTx(methods.emitVerifyArchCallToRevertibleRandom())

        let res = await testCall(methods.verifyArchCallToRevertibleRandom())
        assert.notEqual(
            res.value,
            '0x0000000000000000000000000000000000000000000000000000000000000000'
        )
        assert.lengthOf(res.value, 66)
    })

    it('calls verifyArchCallToVerifyCOAOwnershipProof', async function() {
        let tx = await web3.eth.getTransactionFromBlock(conf.startBlockHeight, 1)
        let bytes = web3.utils.hexToBytes('f853c18088f8d6e0586b0a20c78365766df842b840b90448f4591df2639873be2914c5560149318b7e2fcf160f7bb8ed13cfd97be2f54e6889606f18e50b2c37308386f840e03a9fff915f57b2164cba27f0206a95')
        let addr = '0x1bacdb569847f31ade07e83d6bb7cefba2b9290b35d5c2964663215e73519cff'

        await testEmitTx(methods.emitVerifyArchCallToVerifyCOAOwnershipProof(tx.to, addr, bytes))

        let res = await testCall(methods.verifyArchCallToVerifyCOAOwnershipProof(tx.to, addr, bytes))
        assert.equal(
            web3.eth.abi.decodeParameter('bool', res.value),
            false,
        )
    })
})