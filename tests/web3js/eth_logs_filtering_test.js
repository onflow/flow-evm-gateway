const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('emit logs and retrieve them using different filters', async () => {
    setTimeout(() => process.exit(1), 19 * 1000) // hack if the ws connection is not closed

    let deployed = await helpers.deployContract("storage")
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
})
