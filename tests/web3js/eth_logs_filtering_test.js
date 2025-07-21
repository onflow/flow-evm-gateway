const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('emit logs and retrieve them using different filters', async () => {
    let latestBlockNumber = await web3.eth.getBlockNumber()
    let latestBlock = await web3.eth.getBlock(latestBlockNumber)

    let blockHashFilter = {
        blockHash: latestBlock.hash,
        address: ['0x0000000071727de22e5e9d8baf0edac6f37da032'],
        topics: ['0x2da466a7b24304f47e87fa2e1e5a81b9831ce54fec19055ce277ca2f39ba42c4']
    }
    let response = await helpers.callRPCMethod('eth_getLogs', [blockHashFilter])
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.deepEqual(response.body.result, [])

    blockHashFilter.blockHash = '0x048641726d25605a990c439b75fcfaa5f6b1691eaa718b72dd71e02a2264f5da'
    response = await helpers.callRPCMethod('eth_getLogs', [blockHashFilter])
    assert.equal(response.status, 200)
    assert.isDefined(response.body.error)
    assert.equal(
        response.body.error.message,
        'failed to get EVM block by ID: ' + blockHashFilter.blockHash + ', with: entity not found'
    )

    let blockRangeFilter = {
        fromBlock: '0x1',
        toBlock: 'latest',
        address: ['0x0000000071727de22e5e9d8baf0edac6f37da032'],
        topics: ['0x2da466a7b24304f47e87fa2e1e5a81b9831ce54fec19055ce277ca2f39ba42c4']
    }
    response = await helpers.callRPCMethod('eth_getLogs', [blockRangeFilter])
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.deepEqual(response.body.result, [])

    let deployed = await helpers.deployContract('storage')
    let contractAddress = deployed.receipt.contractAddress

    let repeatA = 10
    const testValues = [
        { A: 1, B: 2 },
        { A: -1, B: -2 },
        { A: repeatA, B: 200 },
        { A: repeatA, B: 300 },
        { A: repeatA, B: 400 },
    ];

    for (const { A, B } of testValues) {
        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(A, B).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })
        assert.equal(res.receipt.status, conf.successStatus)

        // filter each event just emitted by both A and B matching the exact event
        const events = await deployed.contract.getPastEvents('Calculated', {
            filter: { numA: A, numB: B },
            fromBlock: conf.startBlockHeight,
            toBlock: 'latest'
        })

        // Assert that the event is found and the result is correct
        assert.equal(events.length, 1)
        assert.equal(events[0].returnValues.sum, (A + B).toString())
    }

    // filter events by A value equal to 10 which should equal to 3 events with different B values
    let events = await deployed.contract.getPastEvents('Calculated', {
        filter: { numA: repeatA },
        fromBlock: 'earliest',
        toBlock: 'latest',
    })

    assert.lengthOf(events, 3)

    // this filters the test values by A = 10 and makes sure the response logs are expected
    testValues
        .filter(v => v.A == repeatA)
        .forEach((ev, i) => {
            let filtered = events[i].returnValues
            assert.equal(filtered.numA, ev.A)
            assert.equal(filtered.numB, ev.B)
            assert.equal(filtered.sum, ev.A + ev.B)
        })

    // make sure all events are returned
    events = await deployed.contract.getPastEvents({
        fromBlock: 'earliest',
        toBlock: 'latest',
    })
    assert.lengthOf(events, testValues.length)

    // don't return any events in a block range that doesn't have events
    events = await deployed.contract.getPastEvents({
        fromBlock: 1,
        toBlock: 2,
    })
    assert.lengthOf(events, 0)

    // filter by value A being 1 or -1
    events = await deployed.contract.getPastEvents('Calculated', {
        filter: { numA: [-1, 1] },
        fromBlock: 'earliest',
        toBlock: 'latest',
    })
    assert.lengthOf(events, 2)
    assert.equal(events[0].returnValues.numB, 2)
    assert.equal(events[1].returnValues.numB, -2)

    latestBlockNumber = await web3.eth.getBlockNumber()
    latestBlock = await web3.eth.getBlock(latestBlockNumber)

    blockHashFilter = {
        blockHash: latestBlock.hash,
        address: [contractAddress],
        topics: [
            '0x76efea95e5da1fa661f235b2921ae1d89b99e457ec73fb88e34a1d150f95c64b',
            '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
            '0x000000000000000000000000000000000000000000000000000000000000000a',
            '0x0000000000000000000000000000000000000000000000000000000000000190'
        ]
    }
    response = await helpers.callRPCMethod('eth_getLogs', [blockHashFilter])
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.deepEqual(
        response.body.result,
        [
            {
                address: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                topics: [
                    '0x76efea95e5da1fa661f235b2921ae1d89b99e457ec73fb88e34a1d150f95c64b',
                    '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
                    '0x000000000000000000000000000000000000000000000000000000000000000a',
                    '0x0000000000000000000000000000000000000000000000000000000000000190'
                ],
                data: '0x000000000000000000000000000000000000000000000000000000000000019a',
                blockNumber: '0xa',
                transactionHash: '0x0c2b2477ab81c9132c5c4fd4f50935bc5807fbf4cf3bf3b69173491b68d2ca8b',
                transactionIndex: '0x0',
                blockHash: latestBlock.hash,
                blockTimestamp: web3.utils.toHex(latestBlock.timestamp),
                logIndex: '0x0',
                removed: false
            }
        ]
    )

    blockRangeFilter = {
        fromBlock: web3.utils.numberToHex(latestBlock.number),
        toBlock: web3.utils.numberToHex(latestBlock.number),
        address: [contractAddress],
        topics: [
            '0x76efea95e5da1fa661f235b2921ae1d89b99e457ec73fb88e34a1d150f95c64b',
            '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
            '0x000000000000000000000000000000000000000000000000000000000000000a',
            '0x0000000000000000000000000000000000000000000000000000000000000190'
        ]
    }
    response = await helpers.callRPCMethod('eth_getLogs', [blockRangeFilter])
    assert.equal(response.status, 200)
    assert.isDefined(response.body)
    assert.deepEqual(
        response.body.result,
        [
            {
                address: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                topics: [
                    '0x76efea95e5da1fa661f235b2921ae1d89b99e457ec73fb88e34a1d150f95c64b',
                    '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
                    '0x000000000000000000000000000000000000000000000000000000000000000a',
                    '0x0000000000000000000000000000000000000000000000000000000000000190'
                ],
                data: '0x000000000000000000000000000000000000000000000000000000000000019a',
                blockNumber: '0xa',
                transactionHash: '0x0c2b2477ab81c9132c5c4fd4f50935bc5807fbf4cf3bf3b69173491b68d2ca8b',
                transactionIndex: '0x0',
                blockHash: latestBlock.hash,
                blockTimestamp: web3.utils.toHex(latestBlock.timestamp),
                logIndex: '0x0',
                removed: false
            }
        ]
    )
})

it('validates max number of allowed topics', async () => {
    let latestBlockNumber = await web3.eth.getBlockNumber()
    let latestBlock = await web3.eth.getBlock(latestBlockNumber)

    let blockRangeFilter = {
        fromBlock: web3.utils.numberToHex(latestBlock.number),
        toBlock: web3.utils.numberToHex(latestBlock.number),
        address: ['0x0000000071727de22e5e9d8baf0edac6f37da032'],
        topics: [
            '0x76efea95e5da1fa661f235b2921ae1d89b99e457ec73fb88e34a1d150f95c64b',
            '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
            '0x000000000000000000000000000000000000000000000000000000000000000a',
            '0x0000000000000000000000000000000000000000000000000000000000000190',
            '0x000000000000000000000000000000000000000000000000000000000000001a',
        ]
    }

    let response = await helpers.callRPCMethod('eth_getLogs', [blockRangeFilter])
    assert.equal(response.status, 200)
    assert.isDefined(response.body.error)
    assert.equal(response.body.error.message, 'invalid argument 0: exceed max topics')

    let blockHashFilter = {
        blockHash: latestBlock.hash,
        address: ['0x0000000071727de22e5e9d8baf0edac6f37da032'],
        topics: [
            '0x76efea95e5da1fa661f235b2921ae1d89b99e457ec73fb88e34a1d150f95c64b',
            '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
            '0x000000000000000000000000000000000000000000000000000000000000000a',
            '0x0000000000000000000000000000000000000000000000000000000000000190',
            '0x000000000000000000000000000000000000000000000000000000000000001a',
        ]
    }

    response = await helpers.callRPCMethod('eth_getLogs', [blockHashFilter])
    assert.equal(response.status, 200)
    assert.isDefined(response.body.error)
    assert.equal(response.body.error.message, 'invalid argument 0: exceed max topics')
})

it('validates max number of allowed sub-topics', async () => {
    let latestBlockNumber = await web3.eth.getBlockNumber()
    let latestBlock = await web3.eth.getBlock(latestBlockNumber)

    let subTopics = []
    for (i = 0; i <= 1000; i++) {
        subTopics.push(
            '0x76efea95e5da1fa661f235b2921ae1d89b99e457ec73fb88e34a1d150f95c64b'
        )
    }

    let blockRangeFilter = {
        fromBlock: web3.utils.numberToHex(latestBlock.number),
        toBlock: web3.utils.numberToHex(latestBlock.number),
        address: ['0x0000000071727de22e5e9d8baf0edac6f37da032'],
        topics: [subTopics]
    }

    let response = await helpers.callRPCMethod('eth_getLogs', [blockRangeFilter])
    assert.equal(response.status, 200)
    assert.isDefined(response.body.error)
    assert.equal(response.body.error.message, 'invalid argument 0: exceed max topics')

    let blockHashFilter = {
        blockHash: latestBlock.hash,
        address: ['0x0000000071727de22e5e9d8baf0edac6f37da032'],
        topics: [subTopics]
    }

    response = await helpers.callRPCMethod('eth_getLogs', [blockHashFilter])
    assert.equal(response.status, 200)
    assert.isDefined(response.body.error)
    assert.equal(response.body.error.message, 'invalid argument 0: exceed max topics')
})
