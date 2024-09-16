const utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

// this test calls different environment and cadenc arch functions. It uses view
// function to call and return the value which it compares with the block on the network,
// behind the scene these causes the local client to make such a call and return the value
// and this test makes sure the on-chain data is same as local-index data. Secondly, this
// test also submits a transaction that emits the result, which checks the local state
// re-execution of transaction, and makes sure both receipt matches (remote and local),
// this in turn tests the local state re-execution.

describe('calls cadence arch functions and block environment functions', function () {

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

    var methods
    var contractAddress

    before(async function() {
        let deployed = await helpers.deployContract('storage')
        contractAddress = deployed.receipt.contractAddress
        methods = deployed.contract.methods
    })

    it('calls blockNumber', async () => {
        await testEmitTx(methods.emitBlockNumber())

        let res = await testCall(methods.blockNumber())
        // todo eth calls are executed at the provided block height, but at that height
        // block environment functions (number, hash etc), will already point to the block proposal
        // which is the next block, not the block provided by height, discuss this problem!
        assert.equal(
            web3.eth.abi.decodeParameter('uint256', res.value),
            res.block.number+1n,
        )
    })

    it('calls blockTime', async function() {
        await testEmitTx(methods.emitBlockTime())

        let res = await testCall(methods.blockTime())

        // todo eth calls are executed at the provided block height, but at that height
        // block environment functions (number, hash etc), will already point to the block proposal
        // which is the next block, not the block provided by height, discuss this problem!
        let prev = await web3.eth.getBlock(res.block.number)

        assert.equal(
            web3.eth.abi.decodeParameter('uint', res.value).toString(),
            (prev.timestamp+1n).toString(),  // investigate why timestamp is increasing by 1
        )
    })

    it('calls blockHash', async function() {
        let b = await web3.eth.getBlock('latest')

        await testEmitTx(methods.emitBlockHash(b.number))

        // todo eth calls are executed at the provided block height, but at that height
        // block environment functions (number, hash etc), will already point to the block proposal
        // which is the next block, not the block provided by height, discuss this problem!
        let res = await testCall(methods.blockHash(b.number+1n))
        assert.equal(
            web3.eth.abi.decodeParameter('bytes32', res.value).toString(),
            res.block.hash.toString(),
        )
    })

    it('calls random', async function() {
        await testEmitTx(methods.emitRandom())

        let res = await testCall(methods.random())
        assert.isNotEmpty(web3.eth.abi.decodeParameter('uint256', res.value).toString())
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