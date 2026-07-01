const web3Utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require("./helpers");
const utils = require("web3-utils");
const web3 = conf.web3

it('check blockchain is linked', async () => {
    // create some blocks
    for (let i = 0; i < 10; i++) {
        let transfer = await helpers.signAndSend({
            from: conf.eoa.address,
            to: web3.eth.accounts.create().address,
            value: utils.toWei("0.00005", "ether"),
            gasPrice: conf.minGasPrice,
            gasLimit: 55_000,
        })
        assert.isNotEmpty(transfer.hash)
    }

    let latest = await web3.eth.getBlockNumber()

    let block = await web3.eth.getBlock(latest)
    let hash = await get(block.parentHash)
    assert.isNotEmpty(hash) // make sure all blocks were checked
})

async function get(hash) {
    let block = await web3.eth.getBlock(hash)
    if (block.number == 0) { // if genesis return hash
        return hash
    }

    if (block == null) {
        assert.fail("block not found for hash", hash)
    }

    return await get(block.parentHash)
}