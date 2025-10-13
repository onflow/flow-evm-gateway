const helpers = require("./helpers");
const { Web3 } = require("web3");
const conf = require("./config");
const { assert } = require("chai");
const storageABI = require('../fixtures/storageABI.json')
const web3 = conf.web3

async function assertFilterLogs(subscription, expectedLogs) {
    let allLogs = []
    return new Promise((res, rej) => {
        subscription.on("error", err => {
            rej(err)
        })

        subscription.on("data", async data => {
            allLogs.push(data)

            if (allLogs.length !== expectedLogs.length) {
                return
            }

            // if logs matches expected logs length,
            // wait for a bit and re-check, so there's no new logs that came in after delay
            await new Promise(res => setTimeout(() => res(), 1000))
            assert.equal(allLogs.length, expectedLogs.length)

            subscription.unsubscribe()

            // after we receive all logs, we make sure each received
            // logs matches the entry in the expected log by all the values
            for (let i = 0; i < expectedLogs.length; i++) {
                let expected = expectedLogs[i]

                // if we have ABI decoded event values as return values
                if (allLogs[i].returnValues != undefined) {
                    for (const key in expected) {
                        let expectedVal = expected[key]
                        assert.isDefined(allLogs[i].returnValues)
                        let actualVal = allLogs[i].returnValues[key]
                        assert.equal(actualVal, expectedVal)
                    }
                } else { // otherwise compare by position
                    let position = 2 // we start at 2 since first two topics are address and event name
                    for (const key in expected) {
                        let expectedVal = expected[key]
                        assert.isDefined(allLogs[i].topics)
                        // convert big int hex values
                        let actualVal = BigInt(allLogs[i].topics[position])
                        if (actualVal & (1n << 255n)) {
                            actualVal -= (1n << 256n) // convert as signed int256 number
                        }

                        assert.equal(actualVal, expectedVal)
                        position++
                    }
                }
            }

            res(allLogs)
        })
    })
}

it('streaming of logs using filters', async () => {
    let contractDeployment = await helpers.deployContract("storage")
    let contractAddress = contractDeployment.receipt.contractAddress

    // we deploy another contract to use for filtering by address
    let contractDeployment2 = await helpers.deployContract("storage")
    let contractAddress2 = contractDeployment2.receipt.contractAddress

    let repeatA = 10
    const testValues = [
        { numA: 1, numB: 2 },
        { numA: -1, numB: -2 },
        { numA: repeatA, numB: 200 },
        { numA: repeatA, numB: 300 },
        { numA: repeatA, numB: 400 },
    ]

    let ws = new Web3("ws://127.0.0.1:8545")

    let storageContract = new ws.eth.Contract(storageABI, contractAddress)
    let storageContract2 = new ws.eth.Contract(storageABI, contractAddress);
    let calculatedEvent = storageContract.events.Calculated

    let rawSubscribe = filter => ws.eth.subscribe('logs', filter)

    // wait for subscription for a bit
    await new Promise((res, rej) => setTimeout(() => res(), 500))

    let allTests = [
        // stream all events
        assertFilterLogs(calculatedEvent({}), testValues),
        // stream only one event that has numA set to -1
        assertFilterLogs(
            calculatedEvent({ filter: { numA: -1 } }),
            testValues.filter(v => v.numA === -1)
        ),
        // stream only events that have numB set to 200
        assertFilterLogs(
            calculatedEvent({ filter: { numB: 200 } }),
            testValues.filter(v => v.numB === 200)
        ),
        // stream events that have numA set to 10 and numB set to 200
        assertFilterLogs(
            calculatedEvent({ filter: { numA: repeatA, numB: 200 } }),
            testValues.filter(v => v.numB === 200 && v.numA === repeatA)
        ),
        // stream only events that have numA value set to 10
        assertFilterLogs(
            calculatedEvent({ filter: { numA: repeatA } }),
            testValues.filter(v => v.numA === repeatA)
        ),
        // stream events that have numB 200 OR 300 value
        assertFilterLogs(
            calculatedEvent({ filter: { numB: [200, 300] } }),
            testValues.filter(v => v.numB === 200 || v.numB === 300)
        ),

        // we also test the raw subscriptions since they allow for specifying raw values

        // stream all events by any contract, we have two same contracts, so we duplicate expected values and in order
        assertFilterLogs(
            await rawSubscribe({}),
            testValues.concat(testValues)
        ),

        // return all values by only a single contract
        assertFilterLogs(
            await rawSubscribe({ address: contractAddress }),
            testValues
        ),

        // get all events and handle from block provided
        assertFilterLogs(
            await rawSubscribe({ address: contractAddress, fromBlock: "0x0" }),
            testValues,
        )
    ]


    // wait for subscription for a bit
    await new Promise((res, rej) => setTimeout(() => res(), 500))

    // produce events by submitting transactions
    for (const { numA, numB } of testValues) {
        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: storageContract.methods.sum(numA, numB).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })
        assert.equal(res.receipt.status, conf.successStatus)
    }

    for (const { numA, numB } of testValues) {
        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress2,
            data: storageContract2.methods.sum(numA, numB).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })
        assert.equal(res.receipt.status, conf.successStatus)
    }

    await Promise.all(allTests)

    // make sure we can also get logs streamed after the transactions were executed (historic)
    await assertFilterLogs(await rawSubscribe({ address: contractAddress, fromBlock: "0x0" }), testValues)

    ws.currentProvider.disconnect()
})

it('should validate max number of topics', async () => {
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

    let ws = new Web3('ws://127.0.0.1:8545')

    try {
        await ws.eth.subscribe('logs', blockRangeFilter)
        assert.fail('should have received response error')
    } catch (err) {
        assert.equal(
            err.innerError.message,
            'invalid argument 1: exceed max topics'
        )
    }

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

    try {
        await ws.eth.subscribe('logs', blockHashFilter)
        assert.fail('should have received response error')
    } catch (err) {
        assert.equal(
            err.innerError.message,
            'invalid argument 1: exceed max topics'
        )
    }

    ws.currentProvider.disconnect()
})

it('should validate max number of addresses', async () => {
    let latestBlockNumber = await web3.eth.getBlockNumber()
    let latestBlock = await web3.eth.getBlock(latestBlockNumber)

    let addresses = []
    for (let i = 0; i <= 1000; i++) {
        addresses.push(
            '0x0000000071727de22e5e9d8baf0edac6f37da032'
        )
    }

    let blockRangeFilter = {
        fromBlock: web3.utils.numberToHex(latestBlock.number),
        toBlock: web3.utils.numberToHex(latestBlock.number),
        address: addresses,
        topics: []
    }

    let ws = new Web3('ws://127.0.0.1:8545')

    try {
        await ws.eth.subscribe('logs', blockRangeFilter)
        assert.fail('should have received response error')
    } catch (err) {
        assert.equal(
            err.innerError.message,
            'exceed max addresses or topics per search position'
        )
    }

    let blockHashFilter = {
        blockHash: latestBlock.hash,
        address: addresses,
        topics: []
    }

    try {
        await ws.eth.subscribe('logs', blockHashFilter)
        assert.fail('should have received response error')
    } catch (err) {
        assert.equal(
            err.innerError.message,
            'exceed max addresses or topics per search position'
        )
    }

    ws.currentProvider.disconnect()
})


it('should validate max number of sub-topics', async () => {
    let latestBlockNumber = await web3.eth.getBlockNumber()
    let latestBlock = await web3.eth.getBlock(latestBlockNumber)

    let subTopics = []
    for (let i = 0; i <= 1000; i++) {
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

    let ws = new Web3('ws://127.0.0.1:8545')

    try {
        await ws.eth.subscribe('logs', blockRangeFilter)
        assert.fail('should have received response error')
    } catch (err) {
        assert.equal(
            err.innerError.message,
            'invalid argument 1: exceed max topics'
        )
    }

    let blockHashFilter = {
        blockHash: latestBlock.hash,
        address: ['0x0000000071727de22e5e9d8baf0edac6f37da032'],
        topics: [subTopics]
    }

    try {
        await ws.eth.subscribe('logs', blockHashFilter)
        assert.fail('should have received response error')
    } catch (err) {
        assert.equal(
            err.innerError.message,
            'invalid argument 1: exceed max topics'
        )
    }

    ws.currentProvider.disconnect()
})
