const utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('should update the value of eth_gasPrice', async () => {
    let gasPrice = await web3.eth.getGasPrice()
    // The surge factor was last set to 100.0
    assert.equal(gasPrice, 100n * conf.minGasPrice)
})

it('should update the value of eth_MaxPriorityFeePerGas', async () => {
    let response = await helpers.callRPCMethod(
        'eth_maxPriorityFeePerGas',
        []
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)
    // Convert hex quantity to BigInt (e.g., "0x3a98" -> 15000n)
    const maxPriorityFeePerGas = BigInt(response.body.result)
    // The surge factor was last set to 100.0
    assert.equal(maxPriorityFeePerGas, 100n * conf.minGasPrice)
})

it('should reject transactions with gas price lower than the updated value', async () => {
    let receiver = web3.eth.accounts.create()
    let transferValue = utils.toWei('2.5', 'ether')

    let gasPrice = await web3.eth.getGasPrice()
    // The surge factor was last set to 100.0
    assert.equal(gasPrice, 100n * conf.minGasPrice)

    // assert that the minimum acceptable gas price
    // has been multiplied by the surge factor
    try {
        await helpers.signAndSend({
            from: conf.eoa.address,
            to: receiver.address,
            value: transferValue,
            gasPrice: gasPrice - 10n, // provide a lower gas price
            gasLimit: 55_000,
        })
        assert.fail('should not have gotten here')
    } catch (e) {
        assert.include(
            e.message,
            `the minimum accepted gas price for transactions is: ${gasPrice}`
        )
    }
})

it('should accept transactions with the updated gas price', async () => {
    let receiver = web3.eth.accounts.create()
    let transferValue = utils.toWei('2.5', 'ether')

    let gasPrice = await web3.eth.getGasPrice()
    // The surge factor was last set to 100.0
    assert.equal(gasPrice, 100n * conf.minGasPrice)

    let transfer = await helpers.signAndSend({
        from: conf.eoa.address,
        to: receiver.address,
        value: transferValue,
        gasPrice: gasPrice, // provide the updated gas price
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

it('should update gas price for eth_feeFistory', async () => {
    let response = await web3.eth.getFeeHistory(10, 'latest', [20])
    console.log('Response: ', response)

    assert.deepEqual(
        response,
        {
            oldestBlock: 1n,
            reward: [
                ['0x3a98'], // 100 * gas price = 15000
                ['0x3a98'], // 100 * gas price = 15000
                ['0x3a98'], // 100 * gas price = 15000
                ['0x3a98'], // 100 * gas price = 15000
                ['0x3a98'], // 100 * gas price = 15000
                ['0x3a98'], // 100 * gas price = 15000
                ['0x3a98'], // 100 * gas price = 15000
                ['0x3a98'], // 100 * gas price = 15000
                ['0x3a98'], // 100 * gas price = 15000
            ],
            baseFeePerGas: [1n, 1n, 1n, 1n, 1n, 1n, 1n, 1n, 1n],
            gasUsedRatio: [0, 0.006205458333333334, 0, 0, 0, 0, 0, 0, 0.00035]
        }
    )
})
