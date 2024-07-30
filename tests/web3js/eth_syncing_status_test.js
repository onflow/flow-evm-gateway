const utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('should retrieve syncing status', async () => {
    let receiver = web3.eth.accounts.create()

    // make sure receiver balance is initially 0
    let receiverWei = await web3.eth.getBalance(receiver.address)
    assert.equal(receiverWei, 0n)

    // get sender balance
    let senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, utils.toWei(conf.fundedAmount, "ether"))

    // Make sure that we are not currently syncing
    let syncInfo = await web3.eth.isSyncing()
    assert.isFalse(syncInfo)

    let transferValue = utils.toWei("0.5", "ether")
    for (let index = 0; index < 10; index++) {
        const signedTx = await conf.eoa.signTransaction(
            {
                from: conf.eoa.address,
                to: receiver.address,
                value: transferValue,
                gasPrice: '0',
                gasLimit: 55000,
            }
        )

        // Send many transactions, without waiting for the result
        // so the ingestion will lag behind.
        web3.eth.sendSignedTransaction(signedTx.rawTransaction)

        syncInfo = await web3.eth.isSyncing()
        if (syncInfo != false) {
            assert.isDefined(syncInfo.startingBlock)
            assert.isDefined(syncInfo.currentBlock)
            assert.isDefined(syncInfo.highestBlock)
            assert.isBelow(Number(syncInfo.currentBlock), Number(syncInfo.highestBlock))

            return
        }
    }
})
