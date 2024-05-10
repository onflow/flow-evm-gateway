// todo add test that uses different log filters
// - specified address and all topics
// - not specified address (all logs)
// - specified address and single topic
// - specified address and multiple topics
// - combinations of from / to blocks


const helpers = require("./helpers");
const {Web3} = require("web3");
const conf = require("./config");
const {assert} = require("chai");
const storageABI = require('../fixtures/storageABI.json')

const timeout = 20

async function assertFilterLogs(contract, filterObj, expectedLogs) {
    const subscription = await contract.events.Calculated(filterObj)

    let subId = new Promise(res => subscription.on("connected", res))

    let allLogs = []
    let logs = new Promise((res, rej) => subscription.on("data", data => {
        allLogs.push(data)
        console.log("### event", data.returnValues, allLogs.length, expectedLogs.length)
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

                    for (const key in expected) {
                        let expectedVal = expected[key]
                        let actualVal = allLogs[i].returnValues[key]
                        console.log(actualVal, expectedVal)
                        assert.equal("####", actualVal, expectedVal)
                    }
                }

                res(allLogs)
            } else if (allLogs.length > expectedLogs.length) {
                rej(allLogs)
            }
        }, 500)
    }))

    let id = await subId
    assert.isString(id)

    return logs
}

it('streaming of logs using filters', async() => {
    let contractDeployment = await helpers.deployContract("storage")
    let contractAddress = contractDeployment.receipt.contractAddress

    let repeatA = 10
    const testValues = [
        { numA: 1, numB: 2 },
        { numA: -1, numB: -2 },
        { numA: repeatA, numB: 200 },
        { numA: repeatA, numB: 300 },
        { numA: repeatA, numB: 400 },
    ]

    let ws = new Web3("ws://127.0.0.1:8545")
    let storageContract = new ws.eth.Contract(storageABI, contractAddress);

    // wait for subscription for a bit
    await new Promise((res, rej) => setTimeout(() => res(), 500))

    let firstBlock = await ws.eth.getBlockNumber()

    let allTests = [
        //assertFilterLogs(storageContract, { fromBlock: firstBlock }, testValues),
        assertFilterLogs(storageContract, { fromBlock: firstBlock+1n }, testValues.splice(1, 5)),
        //assertFilterLogs(storageContract, { fromBlock: firstBlock+2n }, testValues.splice(0, 2)),
        //assertFilterLogs(storageContract, { fromBlock: firstBlock+3n }, testValues.splice(0, 3)),
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

    await Promise.all(allTests)
    await ws.eth.clearSubscriptions()

    process.exit(0)
}).timeout(timeout*1000)
