const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('should retrieve transaction traces', async () => {
    let deployed = await helpers.deployContract('storage')
    let contractAddress = deployed.receipt.contractAddress

    assert.equal(deployed.receipt.status, conf.successStatus)

    let receipt = await web3.eth.getTransactionReceipt(deployed.receipt.transactionHash)
    assert.equal(receipt.contractAddress, contractAddress)

    response = await helpers.callRPCMethod('debug_traceTransaction', [receipt.transactionHash])
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    let txTrace = response.body.result
    assert.equal(txTrace.from, '0xfacf71692421039876a5bb4f10ef7a439d8ef61e')
    assert.equal(txTrace.gas, '0x158808')
    assert.equal(txTrace.gasUsed, '0x152a63')
    assert.equal(txTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.lengthOf(txTrace.input, 12230n)
    assert.lengthOf(txTrace.output, 12180n)
    assert.equal(txTrace.value, '0x0')
    assert.equal(txTrace.type, 'CREATE')

    let updateData = deployed.contract.methods.store(100n).encodeABI()
    let res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: updateData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    receipt = await web3.eth.getTransactionReceipt(res.receipt.transactionHash)

    response = await helpers.callRPCMethod('debug_traceTransaction', [receipt.transactionHash])
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    txTrace = response.body.result
    assert.equal(txTrace.from, '0xfacf71692421039876a5bb4f10ef7a439d8ef61e')
    assert.equal(txTrace.gas, '0x72f1')
    assert.equal(txTrace.gasUsed, '0x6854')
    assert.equal(txTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(
        txTrace.input,
        updateData
    )
    assert.equal(txTrace.value, '0x0')
    assert.equal(txTrace.type, 'CALL')

    response = await helpers.callRPCMethod('debug_traceBlockByNumber', [web3.utils.toHex(receipt.blockNumber)])
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    let txTraces = response.body.result
    assert.lengthOf(txTraces, 2) // the 2nd tx trace is from the transfer of fees to coinbase
    assert.deepEqual(
        txTraces,
        [
            {
                txHash: '0xd05c80d3de91b3658ebd10427899d18d9de404d3e981565e15950a64f57ab849',
                result: {
                    from: '0xfacf71692421039876a5bb4f10ef7a439d8ef61e',
                    gas: '0x72f1',
                    gasUsed: '0x6854',
                    to: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                    input: '0x6057361d0000000000000000000000000000000000000000000000000000000000000064',
                    value: '0x0',
                    type: 'CALL'
                }
            },
            {
                txHash: '0xd91d32eaaf47f4861d08a09ddcbfe0997e7b48d3472923a5fbbee7e726cc3215',
                result: {
                    from: '0x0000000000000000000000030000000000000000',
                    gas: '0x5b04',
                    gasUsed: '0x5208',
                    to: '0x658bdf435d810c91414ec09147daa6db62406379',
                    input: '0x',
                    value: '0x3d2138',
                    type: 'CALL'
                }
            }
        ]
    )

    response = await helpers.callRPCMethod('debug_traceBlockByHash', [web3.utils.toHex(receipt.blockHash)])
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    txTraces = response.body.result
    assert.lengthOf(txTraces, 2) // the 2nd tx trace is from the transfer of fees to coinbase
    assert.deepEqual(
        txTraces,
        [
            {
                txHash: '0xd05c80d3de91b3658ebd10427899d18d9de404d3e981565e15950a64f57ab849',
                result: {
                    from: '0xfacf71692421039876a5bb4f10ef7a439d8ef61e',
                    gas: '0x72f1',
                    gasUsed: '0x6854',
                    to: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                    input: '0x6057361d0000000000000000000000000000000000000000000000000000000000000064',
                    value: '0x0',
                    type: 'CALL'
                }
            },
            {
                txHash: '0xd91d32eaaf47f4861d08a09ddcbfe0997e7b48d3472923a5fbbee7e726cc3215',
                result: {
                    from: '0x0000000000000000000000030000000000000000',
                    gas: '0x5b04',
                    gasUsed: '0x5208',
                    to: '0x658bdf435d810c91414ec09147daa6db62406379',
                    input: '0x',
                    value: '0x3d2138',
                    type: 'CALL'
                }
            }
        ]
    )

    let callData = deployed.contract.methods.retrieve().encodeABI()
    let traceCall = {
        from: conf.eoa.address,
        to: contractAddress,
        gas: '0x75ab',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
        value: '0x0',
        data: callData,
    }
    response = await helpers.callRPCMethod('debug_traceCall', [traceCall, 'latest'])
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    let callTrace = response.body.result
    assert.equal(callTrace.from, '0xfacf71692421039876a5bb4f10ef7a439d8ef61e')
    assert.equal(callTrace.gas, '0x0')
    assert.equal(callTrace.gasUsed, '0x0')
    assert.equal(callTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(
        callTrace.output,
        '0x0000000000000000000000000000000000000000000000000000000000000064'
    )
    assert.equal(callTrace.value, '0x0')
    assert.equal(callTrace.type, 'CALL')
})
