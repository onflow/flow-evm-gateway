const web3Utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('transfer flow between two EOA accounts', async() => {
    let receiver = web3.eth.accounts.create()

    console.log(receiver.address)

})