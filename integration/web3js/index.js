const { Web3 } = require('web3');
const web3Utils = require('web3-utils')
const fs = require('fs');
const web3 = new Web3("http://localhost:8545");
const storageABI = require("./storageABI.json");


/*
* This is a demonstration of web3.js usage with the EVM Gateway
* In order to run this test you have to make sure the user address (EOA) is funded
* with at least 9 Flow.
* */

let userAddress = "0xFACF71692421039876a5BB4F10EF7A439D8ef61E"
let userAccount = web3.eth.accounts.privateKeyToAccount("0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442");

(async function () {

    // get the latest block
    let block = await web3.eth.getBlockNumber()
    console.log("🎉 Latest block is:", block)

    // get the balance of the EOA account
    let balance = await getBalance()
    console.log("🎉 EOA balance is:", balance)
    // if (balance < 9) {
    //     throw("make sure your EOA is funded with at least 9 Flow")
    // }

    // deploy the test contract, signed by EOA
    let address = await deployContract()
    console.log("🎉 Contract Storage was deployed to:", address)

    // call function on the Storage contract
    let storage = new web3.eth.Contract(storageABI, address)
    let result = await storage.methods.retrieve().call()

    console.log("🎉 Storage contract call Retrieve() returned:", result)

    let newValue = 100
    // store a value in the contract
    let signed = await userAccount.signTransaction({
        from: userAccount.address,
        to: address,
        data: storage.methods.store(newValue).encodeABI(),
        value: '0',
        gasPrice: '1',
    })

    // send signed transaction that stores a new value
    result = await web3.eth.sendSignedTransaction(signed.rawTransaction)
    console.log("🎉 Set a new value using Store() contract function, with tx hash:", result.transactionHash)

    // get transaction result
    result = await web3.eth.getTransaction(result.transactionHash)
    console.log(`🎉 Submitted transaction from ${result.from} to ${result.to} with nonce ${result.nonce}`)

    // call the new stored value
    result = await storage.methods.retrieve().call()
    console.log("🎉 Storage contract call Retrieve() returned:", result)

    // try to filter events by the value stored, which is an indexed value in the event and can be defined as a topic
    let lastBlock = await web3.eth.getBlockNumber()
    let events = await storage.getPastEvents("NewStore", {
        fromBlock: block,
        toBlock: lastBlock,
        topics: [
            null, // wildcard for method name
            null, // wildcard for caller
            web3Utils.padLeft(web3Utils.toHex(newValue), 64) // hex value left-padded matching our new value to filter against
        ]
    })

    console.log("🎉 Got events, the value was set by the address:", events[0].returnValues["0"])

    // should not match a wrong value
    events = await storage.getPastEvents("NewStore", {
        fromBlock: block,
        toBlock: lastBlock,
        topics: [
            null, // wildcard for method name
            null, // wildcard for caller
            web3Utils.padLeft(web3Utils.toHex(newValue+100), 64) // hex value left-padded matching our new value to filter against
        ]
    })

    if (events.length === 0) {
        console.log("🎉 No events matching value that was not set")
    }

})()

async function getBalance() {
    return web3.eth.getBalance(userAddress).then(wei => {
        return web3Utils.fromWei(wei, 'ether')
    })
}

async function deployContract() {
    let storageCode = await fs.promises.readFile("./storage.byte", 'utf8')

    let counter = new web3.eth.Contract(storageABI)

    let data = counter
        .deploy({ data: `0x${storageCode}` })
        .encodeABI()

    let signed = await userAccount.signTransaction({
        from: userAccount.address,
        data: data,
        value: '0',
        gasPrice: '0',
    })

    return new Promise((res, rej) => {
        web3.eth.sendSignedTransaction(signed.rawTransaction)
            .on("error", err => console.log("err", err))
            .on("receipt", r => res(r.contractAddress))
    })
}
