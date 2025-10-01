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
    assert.equal(txTrace.gas, 1200498)
    assert.equal(txTrace.failed, false)
    assert.lengthOf(txTrace.returnValue, 10236)
    assert.deepEqual(
        txTrace.structLogs[0],
        {
            pc: 0,
            op: 'PUSH1',
            gas: 1079409,
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
    assert.equal(txTrace.gas, '0x127b39')
    assert.equal(txTrace.gasUsed, '0x125172')
    assert.equal(txTrace.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.lengthOf(txTrace.input, 10454)
    assert.lengthOf(txTrace.output, 10236)
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
            PUSH1: 3,
            MSTORE: 1,
            PUSH2: 4,
            PUSH0: 4,
            DUP2: 3,
            SWAP1: 3,
            SSTORE: 2,
            POP: 2,
            PUSH20: 3,
            EXP: 1,
            SLOAD: 1,
            MUL: 2,
            NOT: 1,
            AND: 2,
            DUP4: 1,
            OR: 1,
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
    assert.equal(txTrace.gas, '0x6f84')
    assert.equal(txTrace.gasUsed, '0x6e29')
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
        { balance: '0x456391823a384734', nonce: 1 }
    )
    assert.deepEqual(
        txTrace.post['0x0000000000000000000000030000000000000000'],
        { balance: '0x408c06' }
    )
    assert.deepEqual(
        txTrace.post['0xfacf71692421039876a5bb4f10ef7a439d8ef61e'],
        { balance: '0x4563918239f7bb2e', nonce: 2 }
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
            gas: '0x6f84',
            gasUsed: '0x6e29',
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
                '0x57': 8,
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
    assert.equal(txTraces[0].txHash, '0x2a2526cfe1c5533b5debbf7942cb16f25b9e7800c5c33c2d85695256d2fa44a5')
    assert.equal(txTraces[0].result.gas, 28201)
    assert.equal(txTraces[0].result.failed, false)
    assert.equal(txTraces[0].result.returnValue, '0x')
    assert.deepEqual(
        txTraces[0].result.structLogs[0],
        { pc: 0, op: 'PUSH1', gas: 7344, gasCost: 3, depth: 1, stack: [] }
    )

    assert.equal(txTraces[1].txHash, '0x34f823d6fcef9cafccf7b15ec97f7e0734b1b97e0b32992d4243d2d580a8d2b4')
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
                txHash: '0x2a2526cfe1c5533b5debbf7942cb16f25b9e7800c5c33c2d85695256d2fa44a5',
                result: {
                    from: '0xfacf71692421039876a5bb4f10ef7a439d8ef61e',
                    gas: '0x6f84',
                    gasUsed: '0x6e29',
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
                txHash: '0x34f823d6fcef9cafccf7b15ec97f7e0734b1b97e0b32992d4243d2d580a8d2b4',
                result: {
                    from: '0x0000000000000000000000030000000000000000',
                    gas: '0x5b04',
                    gasUsed: '0x5208',
                    to: '0x658bdf435d810c91414ec09147daa6db62406379',
                    input: '0x',
                    value: '0x408c06',
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
    assert.equal(txTraces[0].txHash, '0x2a2526cfe1c5533b5debbf7942cb16f25b9e7800c5c33c2d85695256d2fa44a5')
    assert.equal(txTraces[0].result.gas, 28201)
    assert.equal(txTraces[0].result.failed, false)
    assert.equal(txTraces[0].result.returnValue, '0x')
    assert.deepEqual(
        txTraces[0].result.structLogs[0],
        { pc: 0, op: 'PUSH1', gas: 7344, gasCost: 3, depth: 1, stack: [] }
    )

    assert.equal(txTraces[1].txHash, '0x34f823d6fcef9cafccf7b15ec97f7e0734b1b97e0b32992d4243d2d580a8d2b4')
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
                txHash: '0x2a2526cfe1c5533b5debbf7942cb16f25b9e7800c5c33c2d85695256d2fa44a5',
                result: {
                    from: '0xfacf71692421039876a5bb4f10ef7a439d8ef61e',
                    gas: '0x6f84',
                    gasUsed: '0x6e29',
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
                txHash: '0x34f823d6fcef9cafccf7b15ec97f7e0734b1b97e0b32992d4243d2d580a8d2b4',
                result: {
                    from: '0x0000000000000000000000030000000000000000',
                    gas: '0x5b04',
                    gasUsed: '0x5208',
                    to: '0x658bdf435d810c91414ec09147daa6db62406379',
                    input: '0x',
                    value: '0x408c06',
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
            gas: '0xb5c3',
            gasUsed: '0x619f',
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
    assert.equal(updateTrace.gas, 28213)
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
    assert.equal(updateTrace.gasUsed, '0x6e35')
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
    assert.equal(callTrace.gasUsed, '0x5bdf')
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
                    balance: '0x4563918239be8804',
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
            gasUsed: '0x5bdf',
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
            DUP1: 5,
            ISZERO: 1,
            PUSH2: 13,
            JUMPI: 5,
            JUMPDEST: 12,
            POP: 9,
            CALLDATASIZE: 1,
            LT: 1,
            PUSH0: 6,
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
    assert.equal(callTrace.gasUsed, '0x5bdf')
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
            gasUsed: '0xb445',
            to: contractAddress.toLowerCase(),
            input: '0xc550f90f',
            output: '0x0000000000000000000000000000000000000000000000000000000000000007',
            calls: [
                {
                    from: contractAddress.toLowerCase(),
                    gas: '0x6cee',
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
    assert.equal(updateTrace.gasUsed, '0x6092')
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

    // assert return value of empty revert data for default struct logger tracer
    let callStoreButRevert = deployed.contract.methods.storeButRevert(150n).encodeABI()
    traceCall = {
        from: conf.eoa.address,
        to: contractAddress,
        data: callStoreButRevert,
        value: '0x0',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
        gas: '0x95ab'
    }
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', { tracer: null }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    let traceResult = response.body.result
    assert.equal(traceResult.gas, 26677)
    assert.equal(traceResult.failed, true)
    assert.equal(traceResult.returnValue, '0x')
    assert.lengthOf(traceResult.structLogs, 138)

    // assert return value of non-empty revert data for default struct logger tracer
    let callCustomError = deployed.contract.methods.customError().encodeABI()
    traceCall = {
        from: conf.eoa.address,
        to: contractAddress,
        data: callCustomError,
        value: '0x0',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
        gas: '0x95ab'
    }
    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', { tracer: null }]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    traceResult = response.body.result
    assert.equal(traceResult.gas, 21786)
    assert.equal(traceResult.failed, true)
    assert.equal(traceResult.returnValue, '0x9195785a00000000000000000000000000000000000000000000000000000000000000050000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000001056616c756520697320746f6f206c6f7700000000000000000000000000000000')
    assert.lengthOf(traceResult.structLogs, 210)
})
