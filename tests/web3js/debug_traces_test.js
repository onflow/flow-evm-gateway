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

    let callTracer = {
        tracer: 'callTracer',
        tracerConfig: {
            onlyTopCall: true
        }
    }
    response = await helpers.callRPCMethod(
        'debug_traceTransaction',
        [receipt.transactionHash, callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    // Assert proper response for `callTracer`
    let txTrace = response.body.result
    assert.equal(txTrace.from, '0xfacf71692421039876a5bb4f10ef7a439d8ef61e')
    assert.equal(txTrace.gas, '0x158808')
    assert.equal(txTrace.gasUsed, '0x152a63')
    assert.equal(txTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.lengthOf(txTrace.input, 12230n)
    assert.lengthOf(txTrace.output, 12180n)
    assert.equal(txTrace.value, '0x0')
    assert.equal(txTrace.type, 'CREATE')

    let jsTracer = '{hist: {}, nops: 0, step: function(log, db) { var op = log.op.toString(); if (this.hist[op]){ this.hist[op]++; } else { this.hist[op] = 1; } this.nops++; }, fault: function(log, db) {}, result: function(ctx) { return this.hist; }}'
    response = await helpers.callRPCMethod(
        'debug_traceTransaction',
        [receipt.transactionHash, { tracer: jsTracer }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    // Assert proper response for custom JavaScript tracer
    txTrace = response.body.result
    assert.deepEqual(
        txTrace,
        {
            PUSH1: 2,
            MSTORE: 1,
            PUSH2: 3,
            PUSH0: 3,
            DUP2: 1,
            SWAP1: 1,
            SSTORE: 1,
            POP: 1,
            DUP1: 1,
            CODECOPY: 1,
            RETURN: 1
        }
    )

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

    response = await helpers.callRPCMethod(
        'debug_traceTransaction',
        [receipt.transactionHash, callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    // Assert proper response for `callTracer`
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

    let prestateTracer = {
        tracer: 'prestateTracer',
        tracerConfig: {
            diffMode: true
        }
    }
    response = await helpers.callRPCMethod(
        'debug_traceTransaction',
        [receipt.transactionHash, prestateTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    // Assert proper response for `prestateTracer`
    txTrace = response.body.result
    assert.deepEqual(
        txTrace.pre['0x0000000000000000000000030000000000000000'],
        { balance: '0x0', nonce: 1 }
    )
    assert.deepEqual(
        txTrace.pre['0xfacf71692421039876a5bb4f10ef7a439d8ef61e'],
        { balance: '0x45639182388d29fe', nonce: 1 }
    )
    assert.deepEqual(
        txTrace.post['0x0000000000000000000000030000000000000000'],
        { balance: '0x3d2138' }
    )
    assert.deepEqual(
        txTrace.post['0xfacf71692421039876a5bb4f10ef7a439d8ef61e'],
        { balance: '0x45639182385008c6', nonce: 2 }
    )

    response = await helpers.callRPCMethod(
        'debug_traceTransaction',
        [receipt.transactionHash, { tracer: '4byteTracer' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    // Assert proper response for `4byteTracer`
    txTrace = response.body.result
    assert.deepEqual(
        txTrace,
        { '0x6057361d-32': 1 }
    )

    response = await helpers.callRPCMethod(
        'debug_traceBlockByNumber',
        [web3.utils.toHex(receipt.blockNumber), callTracer]
    )
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

    response = await helpers.callRPCMethod(
        'debug_traceBlockByHash',
        [web3.utils.toHex(receipt.blockHash), callTracer]
    )
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
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    let callTrace = response.body.result
    assert.equal(callTrace.from, '0xfacf71692421039876a5bb4f10ef7a439d8ef61e')
    assert.equal(callTrace.gas, '0x75ab')
    assert.equal(callTrace.gasUsed, '0x6662')
    assert.equal(callTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(
        callTrace.output,
        '0x0000000000000000000000000000000000000000000000000000000000000064'
    )
    assert.equal(callTrace.value, '0x0')
    assert.equal(callTrace.type, 'CALL')

    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', prestateTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    // Assert proper response for `prestateTracer`
    txTrace = response.body.result
    assert.deepEqual(
        txTrace,
        { post: {}, pre: {} }
    )

    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', { tracer: '4byteTracer' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    // Assert proper response for `4byteTracer`
    txTrace = response.body.result
    assert.deepEqual(
        txTrace,
        { '0x2e64cec1-0': 1 }
    )

    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', { tracer: jsTracer }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    // Assert proper response for custom JavaScript tracer
    txTrace = response.body.result
    assert.deepEqual(
        txTrace,
        {
            PUSH1: 7,
            MSTORE: 2,
            CALLVALUE: 1,
            DUP1: 7,
            ISZERO: 1,
            PUSH2: 14,
            JUMPI: 6,
            JUMPDEST: 13,
            POP: 9,
            CALLDATASIZE: 1,
            LT: 1,
            PUSH0: 5,
            CALLDATALOAD: 1,
            SHR: 1,
            PUSH4: 4,
            GT: 3,
            EQ: 1,
            JUMP: 8,
            SLOAD: 1,
            SWAP1: 7,
            MLOAD: 2,
            SWAP2: 4,
            DUP3: 2,
            ADD: 2,
            DUP4: 1,
            DUP5: 1,
            DUP2: 2,
            SWAP3: 1,
            SUB: 1,
            RETURN: 1
        }
    )
})
