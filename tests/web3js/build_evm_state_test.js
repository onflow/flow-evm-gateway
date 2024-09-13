const utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('should handle a large number of EVM interactions', async () => {
    let latest = await web3.eth.getBlockNumber()
    assert.equal(latest, 2n)

    let eoaCount = 20
    let accounts = []

    // Generate 20 EOAs
    // Fund them with some arbitrary number of tokens
    // Make them do transfers to each other

    for (let i = 0; i < eoaCount; i++) {
        let receiver = web3.eth.accounts.create()

        let transferValue = utils.toWei('0.15', 'ether')
        let transfer = await helpers.signAndSend({
            from: conf.eoa.address,
            to: receiver.address,
            value: transferValue,
            gasPrice: conf.minGasPrice,
            gasLimit: 21_000,
        })

        assert.equal(transfer.receipt.status, conf.successStatus)
        assert.equal(transfer.receipt.from, conf.eoa.address)
        assert.equal(transfer.receipt.to, receiver.address)

        // check balance was moved
        let receiverWei = await web3.eth.getBalance(receiver.address)
        assert.equal(receiverWei, transferValue)

        accounts.push(receiver)
    }

    let senderBalance = await web3.eth.getBalance(conf.eoa.address)
    assert.equal(senderBalance, 1999999999937000000n)

    let transferAmounts = ['0.01', '0.03', '0.05']
    for (let i = 0; i < eoaCount; i++) {
        let sender = accounts[i]

        for (let j = 0; j < 3; j++) {
            let receiver = randomItem(accounts)

            let amount = randomItem(transferAmounts)
            let transferValue = utils.toWei(amount, 'ether')
            let transfer = await helpers.signAndSendFrom(sender, {
                from: sender.address,
                to: receiver.address,
                value: transferValue,
                gasPrice: conf.minGasPrice,
                gasLimit: 21_000,
            })

            assert.equal(transfer.receipt.status, conf.successStatus)
            assert.equal(transfer.receipt.from, sender.address)
            assert.equal(transfer.receipt.to, receiver.address)
        }
    }

    latest = await web3.eth.getBlockNumber()
    assert.equal(latest, 82n)

    for (let i = 0; i < eoaCount; i++) {
        let sender = accounts[i]

        let deployed = await helpers.deployContractFrom(sender, 'storage')
        let contractAddress = deployed.receipt.contractAddress

        assert.equal(deployed.receipt.status, conf.successStatus)
        assert.isString(contractAddress)
        assert.equal(deployed.receipt.from, sender.address)

        let storeNumber = Math.floor(Math.random() * 10_000)
        // set the value on the contract, to its current value
        let updateData = deployed.contract.methods.store(storeNumber).encodeABI()
        // store a value in the contract
        let res = await helpers.signAndSendFrom(sender, {
            from: sender.address,
            to: contractAddress,
            data: updateData,
            value: '0',
            gasPrice: conf.minGasPrice,
        })
        assert.equal(res.receipt.status, conf.successStatus)

        sender = randomItem(accounts)
        let sumA = Math.floor(Math.random() * 10_000)
        let sumB = Math.floor(Math.random() * 100_000)
        res = await helpers.signAndSendFrom(sender, {
            from: sender.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(sumA, sumB).encodeABI(),
            gas: 55_000,
            gasPrice: conf.minGasPrice
        })
        assert.equal(res.receipt.status, conf.successStatus)
    }

    latest = await web3.eth.getBlockNumber()
    assert.equal(latest, 142n)
}).timeout(120*1000)

function randomItem(items) {
    return items[Math.floor(Math.random() * items.length)]
}
