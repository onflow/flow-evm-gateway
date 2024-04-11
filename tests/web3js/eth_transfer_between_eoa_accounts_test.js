const utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('transfer flow between two EOA accounts', async() => {
    let receiver = web3.eth.accounts.create()

    // make sure receiver balance is initially 0
    let receiverWei = await web3.eth.getBalance(receiver.address)
    assert.equal(receiverWei, 0n)

    // get sender balance
    let senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, utils.toWei(conf.fundedAmount, "ether"))

    let transferValue = utils.toWei("0.5", "ether")
    let transfer = await helpers.signAndSend({
        from: conf.eoa.address,
        to: receiver.address,
        value: transferValue,
        gasPrice: '0',
        gasLimit: 55000,
    })
    assert.equal(transfer.receipt.status, conf.successStatus)
    assert.equal(transfer.receipt.from, conf.eoa.address)
    assert.equal(transfer.receipt.to, receiver.address)

    // check balance was moved
    receiverWei = await web3.eth.getBalance(receiver.address)
    assert.equal(receiverWei, transferValue)

    senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, utils.toWei(conf.fundedAmount, "ether")-transferValue)

    // make sure latest block includes the transfer tx
    let latest = await web3.eth.getBlockNumber()
    let transferTx = await web3.eth.getTransactionFromBlock(latest, 0)
    assert.equal(transferTx.hash, transfer.receipt.transactionHash)
    assert.equal(transferTx.value, transferValue)

}).timeout(10*1000)