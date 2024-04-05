const { Web3 } = require('web3')
const web3Utils = require('web3-utils')
const fs = require('fs')
const storageABI = require("../storageABI.json")
const { assert } = require('chai')

const web3 = new Web3("http://localhost:8545")

let userAddress = "0xFACF71692421039876a5BB4F10EF7A439D8ef61E"
let userAccount = web3.eth.accounts.privateKeyToAccount("0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442");

describe('JSON RPC API Specification acceptance test', () => {
    describe('Web3Eth', () => {
        let storageAddress
        // setup some contracts and accounts
        before(async function () {
            console.log("setting up tests")

            let receipt = await deployContract()
            assert.isNotNull(receipt)

            storageAddress = receipt.contractAddress
        })


        it('get block', async () => {
            let height = await web3.eth.getBlockNumber()
            console.log("height", height)
            assert.equal(height, 3)

            let block = await web3.eth.getBlock(3)
            console.log(block)

            assert.notDeepEqual(block, {})
            assert.isString(block.hash)
            assert.isString(block.parentHash)
            assert.isString(block.logsBloom)

            let blockHash = await web3.eth.getBlock(block.hash)
            assert.deepEqual(block, blockHash)
        })

        it('get balance', async() => {
            let wei = await web3.eth.getBalance(userAddress)
            assert.isNotNull(wei)

            let flow = web3Utils.fromWei(wei, 'ether')
            assert.isAbove(flow, 1)
        })

    })

})

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

    return web3.eth.sendSignedTransaction(signed.rawTransaction)
}