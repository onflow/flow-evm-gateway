const { assert } = require('chai')
const conf = require('./config')
const web3 = conf.web3

it('should not contain transactions from EVM.dryCall & COA.dryCall', async () => {
    // this test relies on the setup of 'test EVM.dryCall & COA.dryCall'
    // found in ../e2e_web3js_test.go

    let latestHeight = await web3.eth.getBlockNumber()
    let block = await web3.eth.getBlock(latestHeight)

    assert.lengthOf(block.transactions, 3)

    // First transaction is the Storage contract deployment
    let tx = await web3.eth.getTransactionFromBlock(latestHeight, 0)
    let receipt = await web3.eth.getTransactionReceipt(tx.hash)
    assert.equal(receipt.contractAddress, '0x99a64c993965f8d69f985b5171bc20065cc32fab')

    // Second transaction is the Storage.storeWithLog(42) contract call
    tx = await web3.eth.getTransactionFromBlock(latestHeight, 1)
    receipt = await web3.eth.getTransactionReceipt(tx.hash)
    assert.equal(receipt.to, '0x99a64c993965f8d69f985b5171bc20065cc32fab')
    assert.equal(
        receipt.logs[0].topics[2],
        '0x000000000000000000000000000000000000000000000000000000000000002a'
    )

    // Third transaction is the creation of a COA
    tx = await web3.eth.getTransactionFromBlock(latestHeight, 2)
    receipt = await web3.eth.getTransactionReceipt(tx.hash)
    assert.equal(receipt.from, '0x0000000000000000000000020000000000000000')

    let logs = await web3.eth.getPastLogs({
        fromBlock: block.number,
        toBlock: block.number
    })
    assert.deepEqual(
        logs,
        [
            {
                address: '0x99a64c993965f8d69f985b5171bc20065cc32fab',
                topics: [
                    '0x043cc306157a91d747b36aba0e235bbbc5771d75aba162f6e5540767d22673c6',
                    '0x000000000000000000000000facf71692421039876a5bb4f10ef7a439d8ef61e',
                    '0x000000000000000000000000000000000000000000000000000000000000002a'
                ],
                data: '0x',
                blockNumber: 4n,
                transactionHash: '0x096b843b983df3a10ed57e260d1e8858c9d768eb607e1baa5525a1e20a757b3d',
                transactionIndex: 1n,
                blockHash: block.hash,
                logIndex: 0n,
                removed: false
            }
        ]
    )
})
