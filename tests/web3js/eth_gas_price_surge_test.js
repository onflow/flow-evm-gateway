const utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('updates the gas price', async () => {
    let gasPrice = await web3.eth.getGasPrice()
    assert.equal(gasPrice, 2n * conf.minGasPrice)

    let receiver = web3.eth.accounts.create()

    // make sure receiver balance is initially 0
    let receiverWei = await web3.eth.getBalance(receiver.address)
    assert.equal(receiverWei, 0n)

    // get sender balance
    let senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, utils.toWei(conf.fundedAmount, 'ether'))

    let txCount = await web3.eth.getTransactionCount(conf.eoa.address)
    assert.equal(0n, txCount)

    let transferValue = utils.toWei('2.5', 'ether')
    // assert that the minimum acceptable gas price has been multiplied by the surge factor
    try {
        let transfer = await helpers.signAndSend({
            from: conf.eoa.address,
            to: receiver.address,
            value: transferValue,
            gasPrice: gasPrice - 10n,
            gasLimit: 55_000,
        })
        assert.fail('should not have gotten here')
    } catch (e) {
        assert.include(
            e.message,
            `the minimum accepted gas price for transactions is: ${gasPrice}`
        )
    }

    let transfer = await helpers.signAndSend({
        from: conf.eoa.address,
        to: receiver.address,
        value: transferValue,
        gasPrice: gasPrice,
        gasLimit: 55_000,
    })
    assert.equal(transfer.receipt.status, conf.successStatus)
    assert.equal(transfer.receipt.from, conf.eoa.address)
    assert.equal(transfer.receipt.to, receiver.address)

    let latestBlockNumber = await web3.eth.getBlockNumber()
    let latestBlock = await web3.eth.getBlock(latestBlockNumber)
    assert.equal(latestBlock.transactions.length, 2)

    let transferTx = await web3.eth.getTransactionFromBlock(latestBlockNumber, 0)
    let transferTxReceipt = await web3.eth.getTransactionReceipt(transferTx.hash)
    assert.equal(transferTxReceipt.effectiveGasPrice, gasPrice)

    let coinbaseFeesTx = await web3.eth.getTransactionFromBlock(latestBlockNumber, 1)
    assert.equal(coinbaseFeesTx.value, transferTxReceipt.gasUsed * gasPrice)
})
