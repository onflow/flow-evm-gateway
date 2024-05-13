const helpers = require("./helpers");
const {Web3} = require("web3");
const conf = require("./config");
const {assert} = require("chai");
const storageABI = require('../fixtures/storageABI.json')

const timeout = 20

async function assertFilterLogs(subscription, expectedLogs) {
    let allLogs = []
    return new Promise((res, rej) => {
        subscription.on("error", err => {
            console.log(err)
            rej(err)
        })

        subscription.on("data", data => {
            allLogs.push(data)

            // we do this timeout as a trick, to wait if we receive more logs than we should
            // since resolving at the expected length right away might miss another
            // log that would unexpectedly come after.
            setTimeout(() => {
                if (allLogs.length === expectedLogs.length) {
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
                } else if (allLogs.length > expectedLogs.length) {
                    rej(allLogs)
                }
            }, 500)
        })
    })
}

it('streaming of logs using filters', async() => {
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
    let calculatedEvent2 = storageContract.events.Calculated

    let rawSubscribe = filter => ws.eth.subscribe('logs', filter)

    let latestBlock = await ws.eth.getBlockNumber()

    // wait for subscription for a bit
    await new Promise((res, rej) => setTimeout(() => res(), 500))

    let allTests = [
        // stream all events
        assertFilterLogs(calculatedEvent({ }), testValues),
        // stream only one event that has numA set to -1
        assertFilterLogs(
            calculatedEvent({ filter: {numA: -1} }),
            testValues.filter(v => v.numA === -1)
        ),
        // stream only events that have numB set to 200
        assertFilterLogs(
            calculatedEvent({ filter: {numB: 200} }),
            testValues.filter(v => v.numB === 200)
        ),
        // stream events that have numA set to 10 and numB set to 200
        assertFilterLogs(
            calculatedEvent({ filter: {numA: repeatA, numB: 200} }),
            testValues.filter(v => v.numB === 200 && v.numA === repeatA)
        ),
        // stream only events that have numA value set to 10
        assertFilterLogs(
            calculatedEvent({ filter: {numA: repeatA} }),
            testValues.filter(v => v.numA === repeatA)
        ),
        // stream events that have numB 200 OR 300 value
        assertFilterLogs(
            calculatedEvent({ filter: {numB: [200, 300]} }),
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
            gas: 1000000,
            gasPrice: 0
        })
        assert.equal(res.receipt.status, conf.successStatus)
    }

    for (const { numA, numB } of testValues) {
        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress2,
            data: storageContract2.methods.sum(numA, numB).encodeABI(),
            gas: 1000000,
            gasPrice: 0
        })
        assert.equal(res.receipt.status, conf.successStatus)
    }

    await Promise.all(allTests)

    // make sure we can also get logs streamed after the transactions were executed (historic)
    //await assertFilterLogs(await rawSubscribe({ address: contractAddress, fromBlock: "0x0" }), testValues)

    await ws.eth.clearSubscriptions()

    process.exit(0)
}).timeout(timeout*1000)
