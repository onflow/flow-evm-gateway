const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

let deployed = null
let contractAddress = null

before(async () => {
    deployed = await helpers.deployContract('storage')
    contractAddress = deployed.receipt.contractAddress

    assert.equal(deployed.receipt.status, conf.successStatus)
})

it('should retrieve transaction traces', async () => {
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
    assert.equal(txTrace.gas, '0x118e0c')
    assert.equal(txTrace.gasUsed, '0x114010')
    assert.equal(txTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.lengthOf(txTrace.input, 9856n)
    assert.lengthOf(txTrace.output, 9806n)
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
    assert.equal(txTrace.gas, '0x72c3')
    assert.equal(txTrace.gasUsed, '0x6827')
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
        { balance: '0x456391823ad876a0', nonce: 1 }
    )
    assert.deepEqual(
        txTrace.post['0x0000000000000000000000030000000000000000'],
        { balance: '0x3d06da' }
    )
    assert.deepEqual(
        txTrace.post['0xfacf71692421039876a5bb4f10ef7a439d8ef61e'],
        { balance: '0x456391823a9b6fc6', nonce: 2 }
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
                txHash: '0x87449feedc004c75c0e8b12d01656f2e28366c7d73b1b5336beae20aaa5033dd',
                result: {
                    from: '0xfacf71692421039876a5bb4f10ef7a439d8ef61e',
                    gas: '0x72c3',
                    gasUsed: '0x6827',
                    to: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                    input: '0x6057361d0000000000000000000000000000000000000000000000000000000000000064',
                    value: '0x0',
                    type: 'CALL'
                }
            },
            {
                txHash: '0x6039ef1f7dc8d40b74f58e502f5b0b535a46c1b4ddd780c23cb97cf4d681bb47',
                result: {
                    from: '0x0000000000000000000000030000000000000000',
                    gas: '0x5b04',
                    gasUsed: '0x5208',
                    to: '0x658bdf435d810c91414ec09147daa6db62406379',
                    input: '0x',
                    value: '0x3d06da',
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
                txHash: '0x87449feedc004c75c0e8b12d01656f2e28366c7d73b1b5336beae20aaa5033dd',
                result: {
                    from: '0xfacf71692421039876a5bb4f10ef7a439d8ef61e',
                    gas: '0x72c3',
                    gasUsed: '0x6827',
                    to: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                    input: '0x6057361d0000000000000000000000000000000000000000000000000000000000000064',
                    value: '0x0',
                    type: 'CALL'
                }
            },
            {
                txHash: '0x6039ef1f7dc8d40b74f58e502f5b0b535a46c1b4ddd780c23cb97cf4d681bb47',
                result: {
                    from: '0x0000000000000000000000030000000000000000',
                    gas: '0x5b04',
                    gasUsed: '0x5208',
                    to: '0x658bdf435d810c91414ec09147daa6db62406379',
                    input: '0x',
                    value: '0x3d06da',
                    type: 'CALL'
                }
            }
        ]
    )

    callTracer = {
        tracer: 'callTracer',
        tracerConfig: {
            onlyTopCall: false
        }
    }

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

    response = await helpers.callRPCMethod(
        'debug_traceTransaction',
        [web3.utils.toHex(res.receipt.transactionHash), callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    txTrace = response.body.result

    assert.deepEqual(
        txTrace,
        {
            from: conf.eoa.address.toLowerCase(),
            gas: '0xc9c7',
            gasUsed: '0x6147',
            to: contractAddress.toLowerCase(),
            input: '0xc550f90f',
            output: '0x0000000000000000000000000000000000000000000000000000000000000006',
            calls: [
                {
                    from: contractAddress.toLowerCase(),
                    gas: '0x6948',
                    gasUsed: '0x2',
                    to: '0x0000000000000000000000010000000000000001',
                    input: '0x53e87d66',
                    output: '0x0000000000000000000000000000000000000000000000000000000000000006',
                    type: 'STATICCALL'
                }
            ],
            value: '0x0',
            type: 'CALL'
        }
    )
})

it('should retrieve call traces', async () => {
    let receipt = await web3.eth.getTransactionReceipt(deployed.receipt.transactionHash)
    assert.equal(receipt.contractAddress, contractAddress)

    let callTracer = {
        tracer: 'callTracer',
        tracerConfig: {
            onlyTopCall: true
        }
    }

    let callData = deployed.contract.methods.store(500).encodeABI()
    let traceCall = {
        from: conf.eoa.address,
        to: contractAddress,
        data: callData,
        value: '0x0',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
        gas: '0x95ab'
    }
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    let updateTrace = response.body.result
    assert.equal(updateTrace.from, '0xfacf71692421039876a5bb4f10ef7a439d8ef61e')
    assert.equal(updateTrace.gas, '0x95ab')
    assert.equal(updateTrace.gasUsed, '0x6833')
    assert.equal(updateTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(
        updateTrace.input,
        '0x6057361d00000000000000000000000000000000000000000000000000000000000001f4'
    )
    assert.equal(updateTrace.value, '0x0')
    assert.equal(updateTrace.type, 'CALL')

    callData = deployed.contract.methods.retrieve().encodeABI()
    traceCall = {
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
    assert.equal(callTrace.gasUsed, '0x5be0')
    assert.equal(callTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(callTrace.input, '0x2e64cec1')
    assert.equal(
        callTrace.output,
        '0x0000000000000000000000000000000000000000000000000000000000000064'
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
        {
            post: {
                '0xfacf71692421039876a5bb4f10ef7a439d8ef61e': {
                    nonce: 4
                }
            },
            pre: {
                '0xfacf71692421039876a5bb4f10ef7a439d8ef61e': {
                    balance: '0x456391823a62702c',
                    nonce: 3
                }
            }
        }
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
    assert.equal(callTrace.gasUsed, '0x5be0')
    assert.equal(callTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(callTrace.input, '0x2e64cec1')
    assert.equal(
        callTrace.output,
        '0x00000000000000000000000000000000000000000000000000000000000003e8'
    )
    assert.equal(callTrace.value, '0x0')
    assert.equal(callTrace.type, 'CALL')

    let updateData = deployed.contract.methods.store(1500).encodeABI()
    let res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: updateData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    let latestHeight = await web3.eth.getBlockNumber()

    // Assert value on previous block
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, web3.utils.toHex(latestHeight - 1n), callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    callTrace = response.body.result
    assert.equal(
        callTrace.output,
        '0x0000000000000000000000000000000000000000000000000000000000000064'
    )

    // Assert value on latest block
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, web3.utils.toHex(latestHeight), callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    callTrace = response.body.result
    assert.equal(
        callTrace.output,
        '0x00000000000000000000000000000000000000000000000000000000000005dc'
    )

    let flowBlockHeightData = deployed.contract.methods.verifyArchCallToFlowBlockHeight().encodeABI()
    traceCall = {
        from: conf.eoa.address,
        to: contractAddress,
        gas: '0xcdd4',
        data: flowBlockHeightData,
        value: '0x0',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
    }

    callTracer = {
        tracer: 'callTracer',
        tracerConfig: {
            onlyTopCall: false
        }
    }

    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, web3.utils.toHex(latestHeight), callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    callTrace = response.body.result
    assert.deepEqual(
        callTrace,
        {
            from: conf.eoa.address.toLowerCase(),
            gas: '0xcdd4',
            gasUsed: '0xbdd4',
            to: contractAddress.toLowerCase(),
            input: '0xc550f90f',
            output: '0x0000000000000000000000000000000000000000000000000000000000000007',
            calls: [
                {
                    from: contractAddress.toLowerCase(),
                    gas: '0x6d44',
                    gasUsed: '0x5c8f',
                    to: '0x0000000000000000000000010000000000000001',
                    input: '0x53e87d66',
                    output: '0x0000000000000000000000000000000000000000000000000000000000000007',
                    type: 'STATICCALL'
                }
            ],
            value: '0x0',
            type: 'CALL'
        }
    )
})
