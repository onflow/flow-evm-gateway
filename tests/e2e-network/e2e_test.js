const { Web3 } = require('web3');
const web3Utils = require('web3-utils')
const assert = require('assert');
const fs = require('fs');
const storageABI = require("./storageABI.json");

let endpoints = {
    local: "http://localhost:3000",
    previewnet: "https://previewnet.evm.nodes.onflow.org",
    migrationnet: "https://migrationtestnet.evm.nodes.onflow.org",
    testnet: "https://testnet.evm.nodes.onflow.org",
}

if (process.env.GENERATE == 1) {
    const web3 = new Web3(endpoints.local) // doesn't matter
    const privateKey = web3.eth.accounts.create().privateKey
    const address = web3.eth.accounts.privateKeyToAccount(privateKey).address

    console.log("Private Key:", privateKey)
    console.log("Address:", address)
    return
}

let rpcHost = process.env.RPC_HOST;
if (rpcHost == "") {
    console.log("You need to set the `RPC_HOST` env variable (local/previewnet/migrationnet)");
    process.exit(1);
}

const web3 = new Web3(endpoints[rpcHost]);

let userPrivateKey = process.env.USER_PRIVATE_KEY;
if (userPrivateKey == "") {
    console.log("You need to set the `USER_PRIVATE_KEY`");
    process.exit(1);
}

const userAccount = web3.eth.accounts.privateKeyToAccount(userPrivateKey);

console.log("Using user account: ", userAccount.address)

describe('Ethereum Contract Deployment and Interaction Tests', async function() {
    this.timeout(0) // Disable timeout since blockchain interactions can be slow
    let initBlock = 0
    let gasPrice = await web3.eth.getGasPrice()

    it('Should get the network ID', async function() {
        const id = await web3.eth.getChainId()
        assert.ok(id, "Network ID should be available")
    })

    it('Should fetch the latest block number', async function() {
        const block = await web3.eth.getBlockNumber()
        initBlock = block
        assert.ok(block, "Should fetch the latest block number")
    })

    it('Should get genesis block', async function() {
        const block = await web3.eth.getBlock(0)
        assert.ok(block, "Should fetch the genesis block")
    })

    it('Get specific block', async function () {
        let block = await web3.eth.getBlock(1, false)
        assert.ok(block)
    })

    it('Should get an account nonce', async function () {
        await assert.doesNotReject(web3.eth.getTransactionCount(userAccount.address))
    })

    it('Should get an gas price', async function () {
        await assert.doesNotReject(web3.eth.getGasPrice())
    })

    it('Should transfer value to itself', async function (){
        let value = 0.01
        let receipt = await transfer(value, userAccount.address) // todo check nonce
        assert.equal(userAccount.address, receipt.to)
        assert.equal(receipt.status, 1n)
    })

    it('Should ensure the EOA is sufficiently funded', async function() {
        const balance = await getBalance(userAccount.address)
        assert.ok(parseFloat(balance) >= 5, "EOA should be funded with at least 9 Ether")
    })

    describe('Contract interactions', async function () {
        const initValue = 1337
        const newValue = 100

        let deployedAddress
        let storage
        let lastBlock

        before(async function () {
            deployedAddress = await deployContract()
            assert.ok(deployedAddress.length > 0, "Contract should be deployed and return an address")
            storage = new web3.eth.Contract(storageABI, deployedAddress)
        })

        it('Should retrieve the value from the Store contract', async function () {
            // Retrieve the new value
            const result = await storage.methods.retrieve().call();
            assert.strictEqual(parseInt(result), initValue, "Retrieve call should return the value stored");
        })

        it('Should store a new value in the Store contract', async function() {
            // store a value in the contract
            let signed = await userAccount.signTransaction({
                from: userAccount.address,
                to: deployedAddress,
                data: storage.methods.store(newValue).encodeABI(),
                value: '0',
                gasPrice: gasPrice,
            })
            let result = await web3.eth.sendSignedTransaction(signed.rawTransaction)
            assert.ok(result.transactionHash)
        })

        it('Should retrieve the new value from the Store contract', async function () {
            // Retrieve the new value
            const result = await storage.methods.retrieve().call()
            assert.strictEqual(parseInt(result), newValue, "Retrieve call should return the new value stored");
        })

        it('Should get all past events that match the contract', async function () {
            lastBlock = await web3.eth.getBlockNumber()

            // try to filter events by the value stored, which is an indexed value in the event and can be defined as a topic
            let events = await storage.getPastEvents("NewStore", {
                fromBlock: initBlock,
                toBlock: lastBlock,
                topics: [
                    null, // wildcard for method name
                    null, // wildcard for caller
                    web3Utils.padLeft(web3Utils.toHex(newValue), 64) // hex value left-padded matching our new value to filter against
                ]
            })

            assert.equal(events[0].returnValues.value, newValue, "the event value should match the new value")
        })

        it('Should not match events with non-matching filter', async function() {
            let events = await storage.getPastEvents("NewStore", {
                fromBlock: initBlock,
                toBlock: lastBlock,
                topics: [
                    null, // wildcard for method name
                    null, // wildcard for caller
                    web3Utils.padLeft(web3Utils.toHex(newValue+100), 64) // hex value left-padded matching our new value to filter against
                ]
            })

            assert.equal(events.length, 0, "should not get any events")
        })

        it('Gets storage at', async function () {
            const slot = 0; // The slot for the 'number' variable

            let stored = await web3.eth.getStorageAt(deployedAddress, slot)
            const value = web3.utils.hexToNumberString(stored);
            assert.equal(value, newValue)
        })

        // todo get the trace for transaction
    })
})

// this test traverses the blockchain and checks the blocks are correct and linked.
describe('Validate blockchain', function () {
    let startHeight = process.env.CHECK_HEIGHT
    if (startHeight == "") {
        console.log("You need to set the `CHECK_HEIGHT` env variable")
        process.exit(1)
    }

    it('traverse blockchain', async function () {
        let first = await web3.eth.getBlock(startHeight)

        await getBlock(first.parentHash, first.number)
    })
})

describe('EVM Gateway load tests', function () {
    this.timeout(0)
    //return // skip unless explicitly run

    it('Submit batch of transactions that transfer value back to same account', async function () {
        let nonce = await web3.eth.getTransactionCount(userAccount.address)
        let value = 0.02
        let batch = 2
        let txs = []

        for (let i = 0; i < batch; i++) {
            console.log("sending request ", i)
            txs.push(transfer(value, userAccount.address, nonce + BigInt(i)))
            await new Promise(res => setTimeout(() => res(), 100))
        }

        await Promise.all(txs)
    })
})


async function getBlock(hash, number) {
    console.log("checking block: ", hash, number)

    let block = await web3.eth.getBlock(hash)
    if (block == null) {
        assert.fail(`missing block ${hash}`)
        return
    }

    if (block.number+1n !== number) {
        assert.fail(`invalid block number ${number} != ${block.number} block hash: ${block.hash}`)
    }

    if (block.transactions != null) {
        for (let hash of block.transactions) {
            let tx = await web3.eth.getTransaction(hash)
            if (tx.blockNumber !== block.number) {
                assert.fail(`invalid transaction ${tx} at block ${number}`)
            }
        }
    }

    await getBlock(block.parentHash, block.number)
}

async function getBalance(addr) {
    return web3.eth.getBalance(addr).then(wei => {
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
        gasPrice: gasPrice,
    })

    let rcp = await web3.eth.sendSignedTransaction(signed.rawTransaction)
    return rcp.contractAddress
}

async function transfer(amount, to, nonce) {
    let tx = {
        from: userAccount.address,
        data: null,
        to: to,
        value: web3.utils.toWei(amount, "ether"),
        gasPrice: gasPrice,
    }
    if (nonce != null) {
        tx.nonce = nonce
    }

    let signed = await userAccount.signTransaction(tx)
    return web3.eth.sendSignedTransaction(signed.rawTransaction)
}
