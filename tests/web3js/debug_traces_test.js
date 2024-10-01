const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('should retrieve call traces', async () => {
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
    assert.equal(callTrace.gasUsed, '0x664b')
    assert.equal(callTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(
        callTrace.output,
        '0x0000000000000000000000000000000000000000000000000000000000000539'
    )
    assert.equal(callTrace.value, '0x0')
    assert.equal(callTrace.type, 'CALL')

    let prestateTracer = {
        tracer: 'prestateTracer',
        tracerConfig: {
            diffMode: true
        }
    }
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

    let jsTracer = '{hist: {}, nops: 0, step: function(log, db) { var op = log.op.toString(); if (this.hist[op]){ this.hist[op]++; } else { this.hist[op] = 1; } this.nops++; }, fault: function(log, db) {}, result: function(ctx) { return this.hist; }}'
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', { tracer: jsTracer }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    // Assert proper response for custom JavaScript tracer
    txTrace = response.body.result
    console.log('Tx Trace: ', txTrace)
    assert.deepEqual(
        txTrace,
        {
            PUSH1: 7,
            MSTORE: 2,
            CALLVALUE: 1,
            DUP1: 6,
            ISZERO: 1,
            PUSH2: 13,
            JUMPI: 5,
            JUMPDEST: 12,
            POP: 9,
            CALLDATASIZE: 1,
            LT: 1,
            PUSH0: 5,
            CALLDATALOAD: 1,
            SHR: 1,
            PUSH4: 3,
            GT: 2,
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

    let callTracerWithStateOverrides = {
        tracer: 'callTracer',
        tracerConfig: {
            onlyTopCall: true
        },
        stateOverrides: {
            [contractAddress]: {
                stateDiff: {
                    '0x0000000000000000000000000000000000000000000000000000000000000000': '0x00000000000000000000000000000000000000000000000000000000000003e8'
                }
            }
        }
    }
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', callTracerWithStateOverrides]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    callTrace = response.body.result
    assert.equal(callTrace.from, '0xfacf71692421039876a5bb4f10ef7a439d8ef61e')
    assert.equal(callTrace.gas, '0x75ab')
    assert.equal(callTrace.gasUsed, '0x664b')
    assert.equal(callTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(
        callTrace.output,
        '0x00000000000000000000000000000000000000000000000000000000000003e8'
    )
    assert.equal(callTrace.value, '0x0')
    assert.equal(callTrace.type, 'CALL')
})
