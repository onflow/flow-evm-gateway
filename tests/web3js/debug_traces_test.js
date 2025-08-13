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

    let response = await helpers.callRPCMethod(
        'debug_traceTransaction',
        [receipt.transactionHash, { tracer: null }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    // Assert proper response for `structLog`
    let txTrace = response.body.result
    assert.equal(txTrace.gas, 1130512)
    assert.equal(txTrace.failed, false)
    assert.lengthOf(txTrace.returnValue, 9806n)
    assert.deepEqual(
        txTrace.structLogs[0],
        {
            pc: 0,
            op: 'PUSH1',
            gas: 1013648,
            gasCost: 3,
            depth: 1,
            stack: []
        }
    )

    let callTracer = {
        tracer: 'callTracer',
        tracerConfig: {
            withLog: true,
            onlyTopCall: false
        }
    }
    response = await helpers.callRPCMethod(
        'debug_traceTransaction',
        [receipt.transactionHash, callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    // Assert proper response for `callTracer`
    txTrace = response.body.result
    assert.equal(txTrace.from, '0xfacf71692421039876a5bb4f10ef7a439d8ef61e')
    assert.equal(txTrace.gas, '0x1167ac')
    assert.equal(txTrace.gasUsed, '0x114010')
    assert.equal(txTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.lengthOf(txTrace.input, 9856n)
    assert.lengthOf(txTrace.output, 9806n)
    assert.isUndefined(txTrace.logs)
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

    let updateData = deployed.contract.methods.storeWithLog(100n).encodeABI()
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
    assert.equal(txTrace.gas, '0x6f9a')
    assert.equal(txTrace.gasUsed, '0x6e3f')
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
        { balance: '0x4098ea' }
    )
    assert.deepEqual(
        txTrace.post['0xfacf71692421039876a5bb4f10ef7a439d8ef61e'],
        { balance: '0x456391823a97ddb6', nonce: 2 }
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
        { '0x6babb224-32': 1 }
    )

    response = await helpers.callRPCMethod(
        'debug_traceTransaction',
        [receipt.transactionHash, { tracer: 'erc7562Tracer' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    // Assert proper response for `erc7562Tracer`
    txTrace = response.body.result
    assert.deepEqual(
        txTrace,
        {
            from: '0xfacf71692421039876a5bb4f10ef7a439d8ef61e',
            gas: '0x6f9a',
            gasUsed: '0x6e3f',
            to: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
            input: '0x6babb2240000000000000000000000000000000000000000000000000000000000000064',
            value: '0x0',
            accessedSlots: {
                reads: {},
                writes: {
                    '0x0000000000000000000000000000000000000000000000000000000000000000': 1
                },
                transientReads: {},
                transientWrites: {}
            },
            extCodeAccessInfo: [],
            usedOpcodes: {
                '0x0': 1,
                '0x33': 1,
                '0x34': 1,
                '0x35': 2,
                '0x36': 2,
                '0x51': 2,
                '0x52': 1,
                '0x55': 1,
                '0x56': 10,
                '0x57': 9,
                '0x5b': 15,
                '0xa3': 1
            },
            contractSize: {},
            outOfGas: false,
            type: 'CALL'
        }
    )

    response = await helpers.callRPCMethod(
        'debug_traceBlockByNumber',
        [web3.utils.toHex(receipt.blockNumber), { tracer: null }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    // Assert proper response for `structLog`
    let txTraces = response.body.result
    assert.equal(txTraces[0].txHash, '0x59a48269f0f57fdbfe53fed4a27d95deee3d12aad62b40d22562ad2270732c8d')
    assert.equal(txTraces[0].result.gas, 28223)
    assert.equal(txTraces[0].result.failed, false)
    assert.equal(txTraces[0].result.returnValue, '0x')
    assert.deepEqual(
        txTraces[0].result.structLogs[0],
        { pc: 0, op: 'PUSH1', gas: 7366, gasCost: 3, depth: 1, stack: [] }
    )

    assert.equal(txTraces[1].txHash, '0x828d174e7fbdd7de2224f0204953356d9ccd5b1783780e27340a745a96d6e9e4')
    assert.equal(txTraces[1].result.gas, 21000)
    assert.equal(txTraces[1].result.failed, false)
    assert.equal(txTraces[1].result.returnValue, '0x')
    assert.deepEqual(txTraces[1].result.structLogs, [])

    response = await helpers.callRPCMethod(
        'debug_traceBlockByNumber',
        [web3.utils.toHex(receipt.blockNumber), callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    txTraces = response.body.result
    assert.lengthOf(txTraces, 2) // the 2nd tx trace is from the transfer of fees to coinbase
    assert.deepEqual(
        txTraces,
        [
            {
                txHash: '0x59a48269f0f57fdbfe53fed4a27d95deee3d12aad62b40d22562ad2270732c8d',
                result: {
                    from: '0xfacf71692421039876a5bb4f10ef7a439d8ef61e',
                    gas: '0x6f9a',
                    gasUsed: '0x6e3f',
                    to: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                    input: '0x6babb2240000000000000000000000000000000000000000000000000000000000000064',
                    logs: [
                        {
                            address: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                            data: '0x',
                            position: '0x0',
                            topics: [
                                '0x043cc306157a91d747b36aba0e235bbbc5771d75aba162f6e5540767d22673c6',
                                '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
                                '0x0000000000000000000000000000000000000000000000000000000000000064'
                            ]
                        }
                    ],
                    value: '0x0',
                    type: 'CALL'
                }
            },
            {
                txHash: '0x828d174e7fbdd7de2224f0204953356d9ccd5b1783780e27340a745a96d6e9e4',
                result: {
                    from: '0x0000000000000000000000030000000000000000',
                    gas: '0x5b04',
                    gasUsed: '0x5208',
                    to: '0x658bdf435d810c91414ec09147daa6db62406379',
                    input: '0x',
                    value: '0x4098ea',
                    type: 'CALL'
                }
            }
        ]
    )

    response = await helpers.callRPCMethod(
        'debug_traceBlockByHash',
        [web3.utils.toHex(receipt.blockHash), { tracer: null }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    // Assert proper response for `structLog`
    txTraces = response.body.result
    assert.equal(txTraces[0].txHash, '0x59a48269f0f57fdbfe53fed4a27d95deee3d12aad62b40d22562ad2270732c8d')
    assert.equal(txTraces[0].result.gas, 28223)
    assert.equal(txTraces[0].result.failed, false)
    assert.equal(txTraces[0].result.returnValue, '0x')
    assert.deepEqual(
        txTraces[0].result.structLogs[0],
        { pc: 0, op: 'PUSH1', gas: 7366, gasCost: 3, depth: 1, stack: [] }
    )

    assert.equal(txTraces[1].txHash, '0x828d174e7fbdd7de2224f0204953356d9ccd5b1783780e27340a745a96d6e9e4')
    assert.equal(txTraces[1].result.gas, 21000)
    assert.equal(txTraces[1].result.failed, false)
    assert.equal(txTraces[1].result.returnValue, '0x')
    assert.deepEqual(txTraces[1].result.structLogs, [])

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
                txHash: '0x59a48269f0f57fdbfe53fed4a27d95deee3d12aad62b40d22562ad2270732c8d',
                result: {
                    from: '0xfacf71692421039876a5bb4f10ef7a439d8ef61e',
                    gas: '0x6f9a',
                    gasUsed: '0x6e3f',
                    to: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                    input: '0x6babb2240000000000000000000000000000000000000000000000000000000000000064',
                    logs: [
                        {
                            address: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                            data: '0x',
                            position: '0x0',
                            topics: [
                                '0x043cc306157a91d747b36aba0e235bbbc5771d75aba162f6e5540767d22673c6',
                                '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
                                '0x0000000000000000000000000000000000000000000000000000000000000064'
                            ]
                        }
                    ],
                    value: '0x0',
                    type: 'CALL'
                }
            },
            {
                txHash: '0x828d174e7fbdd7de2224f0204953356d9ccd5b1783780e27340a745a96d6e9e4',
                result: {
                    from: '0x0000000000000000000000030000000000000000',
                    gas: '0x5b04',
                    gasUsed: '0x5208',
                    to: '0x658bdf435d810c91414ec09147daa6db62406379',
                    input: '0x',
                    value: '0x4098ea',
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
            gas: '0xb56b',
            gasUsed: '0x6147',
            to: contractAddress.toLowerCase(),
            input: '0xc550f90f',
            output: '0x0000000000000000000000000000000000000000000000000000000000000006',
            calls: [
                {
                    from: contractAddress.toLowerCase(),
                    gas: '0x553d',
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
            withLog: true,
            onlyTopCall: true
        }
    }

    let callData = deployed.contract.methods.storeWithLog(500).encodeABI()
    let traceCall = {
        from: conf.eoa.address,
        to: contractAddress,
        data: callData,
        value: '0x0',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
        gas: '0x95ab'
    }

    let response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', { tracer: null }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    let updateTrace = response.body.result
    assert.equal(updateTrace.gas, 28235)
    assert.equal(updateTrace.failed, false)
    assert.equal(updateTrace.returnValue, '0x')
    assert.deepEqual(
        updateTrace.structLogs[0],
        { pc: 0, op: 'PUSH1', gas: 17099, gasCost: 3, depth: 1, stack: [] }
    )
    assert.deepEqual(
        updateTrace.structLogs[1],
        {
            pc: 2,
            op: 'PUSH1',
            gas: 17096,
            gasCost: 3,
            depth: 1,
            stack: ['0x80']
        }
    )

    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    updateTrace = response.body.result
    assert.equal(updateTrace.from, '0xfacf71692421039876a5bb4f10ef7a439d8ef61e')
    assert.equal(updateTrace.gas, '0x95ab')
    assert.equal(updateTrace.gasUsed, '0x6e4b')
    assert.equal(updateTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(
        updateTrace.input,
        '0x6babb22400000000000000000000000000000000000000000000000000000000000001f4'
    )
    assert.deepEqual(
        updateTrace.logs,
        [
            {
                address: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                data: '0x',
                position: '0x0',
                topics: [
                    '0x043cc306157a91d747b36aba0e235bbbc5771d75aba162f6e5540767d22673c6',
                    '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
                    '0x00000000000000000000000000000000000000000000000000000000000001f4'
                ]
            }
        ]
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
                    balance: '0x456391823a5ede1c',
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

    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', { tracer: 'erc7562Tracer' }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    // Assert proper response for `erc7562Tracer`
    txTrace = response.body.result
    assert.deepEqual(
        txTrace,
        {
            from: '0xfacf71692421039876a5bb4f10ef7a439d8ef61e',
            gas: '0x75ab',
            gasUsed: '0x5be0',
            to: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
            input: '0x2e64cec1',
            output: '0x0000000000000000000000000000000000000000000000000000000000000064',
            value: '0x0',
            accessedSlots: {
                reads: {
                    '0x0000000000000000000000000000000000000000000000000000000000000000': [
                        '0x0000000000000000000000000000000000000000000000000000000000000064'
                    ]
                },
                writes: {},
                transientReads: {},
                transientWrites: {}
            },
            extCodeAccessInfo: [],
            usedOpcodes: {
                '0x34': 1,
                '0x35': 1,
                '0x36': 1,
                '0x51': 2,
                '0x52': 2,
                '0x54': 1,
                '0x56': 8,
                '0x57': 5,
                '0x5b': 12,
                '0xf3': 1
            },
            contractSize: {},
            outOfGas: false,
            type: 'CALL'
        }
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
            withLog: false,
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
            gasUsed: '0xb3ed',
            to: contractAddress.toLowerCase(),
            input: '0xc550f90f',
            output: '0x0000000000000000000000000000000000000000000000000000000000000007',
            calls: [
                {
                    from: contractAddress.toLowerCase(),
                    gas: '0x6d44',
                    gasUsed: '0x52a8',
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

    callTracer = {
        tracer: 'callTracer',
        tracerConfig: {
            withLog: true,
            onlyTopCall: false
        }
    }

    callData = deployed.contract.methods.sum(500, 100).encodeABI()
    traceCall = {
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

    updateTrace = response.body.result
    assert.equal(updateTrace.from, '0xfacf71692421039876a5bb4f10ef7a439d8ef61e')
    assert.equal(updateTrace.gas, '0x95ab')
    assert.equal(updateTrace.gasUsed, '0x6094')
    assert.equal(updateTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(
        updateTrace.input,
        '0x9967062d00000000000000000000000000000000000000000000000000000000000001f40000000000000000000000000000000000000000000000000000000000000064'
    )
    assert.deepEqual(
        updateTrace.logs,
        [
            {
                address: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                data: '0x0000000000000000000000000000000000000000000000000000000000000258',
                position: '0x0',
                topics: [
                    '0x76efea95e5da1fa661f235b2921ae1d89b99e457ec73fb88e34a1d150f95c64b',
                    '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
                    '0x00000000000000000000000000000000000000000000000000000000000001f4',
                    '0x0000000000000000000000000000000000000000000000000000000000000064'
                ]
            }
        ]
    )
    assert.equal(updateTrace.value, '0x0')
    assert.equal(updateTrace.type, 'CALL')
})
