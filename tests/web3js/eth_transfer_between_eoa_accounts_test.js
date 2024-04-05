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
    assert.equal(senderBalance, conf.fundedAmount)

    let transferValue = utils.toWei("0.5", "ether")
    let transfer = await conf.eoa.signTransaction({
        from: conf.eoa.address,
        value: transferValue,
        gasPrice: '0',
        gasLimit: 50000,
    })

    // make sure receipt is correct
    let receipt = await web3.eth.sendSignedTransaction(transfer.rawTransaction)
    assert.equal(receipt.status, conf.successStatus)
    assert.equal(receipt.from, conf.eoa.address)
    assert.equal(receipt.to, receiver.address)

    // check balance was moved
    receiverWei = await web3.eth.getBalance(receiver.address)
    assert.equal(receiverWei, transferValue)

    // make sure latest block includes the transfer tx
    let latest = await web3.eth.getBlockNumber()
    let transferTx = await web3.eth.getTransactionFromBlock(latest, 0)
    assert.equal(transferTx.hash, transfer.transactionHash)
})