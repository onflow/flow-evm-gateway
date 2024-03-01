const { Web3 } = require('web3')
const chai = import('chai')
const web3Utils = require('web3-utils')
const fs = require('fs')
const storageABI = require("../storageABI.json")
let assert = chai.assert

const web3 = new Web3("http://localhost:8545")

describe('JSON RPC API Specification acceptance test', () => {
    describe('Web3Eth', () => {

        it('get block', async () => {
            let height = await web3.eth.getBlockNumber()
            assert.isNotEmpty(height)

            let block = await web3.eth.getBlock(height)

            assert.isNotEmpty(block.hash)
            assert.isNotEmpty(block.parentHash)
            assert.isNotEmpty(block.logsBloom)

            let blockHash = await web3.eth.getBlock(block.hash)
            assert.deepEqual(block, blockHash)
        })

    })

})