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

    // get balance at special block tags
    let blockTags = ["latest", "pending", "safe", "finalized"]
    for (let blockTag of blockTags) {
        receiverWei = await web3.eth.getBalance(receiver.address, blockTag)
        assert.equal(receiverWei, transferValue)
    }

    // get balance at earliest block tag
    receiverWei = await web3.eth.getBalance(receiver.address, "earliest")
    assert.equal(receiverWei, transferValue)

    // get balance at past block
    receiverWei = await web3.eth.getBalance(receiver.address, latest - 1n)
    assert.equal(receiverWei, 0n)

    // get balance at non-existent block number
    try {
        receiverWei = await web3.eth.getBalance(receiver.address, latest + 15n)
    } catch(error) {
        assert.match(error.message, /entity not found/)
    }

    // get balance at latest block number
    receiverWei = await web3.eth.getBalance(receiver.address, latest)
    assert.equal(receiverWei, transferValue)
}).timeout(10*1000)
