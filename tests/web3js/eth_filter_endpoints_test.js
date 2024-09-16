const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

let deployed = null
let contractAddress = null

before(async () => {
    deployed = await helpers.deployContract('storage')
    contractAddress = deployed.receipt.contractAddress

    assert.equal(deployed.receipt.status, conf.successStatus)
})

describe('eth_uninstallFilter', async () => {
    it('should return false for missing filter', async () => {
        let response = await helpers.callRPCMethod('eth_uninstallFilter', ['0xffa1'])

        assert.equal(response.status, 200)
        assert.isFalse(response.body.result)
    })

    it('should return true for existing filter', async () => {
        let response = await helpers.callRPCMethod(
            'eth_newFilter',
            [{ 'address': contractAddress }]
        )

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        response = await helpers.callRPCMethod('eth_uninstallFilter', [filterID])

        assert.equal(response.status, 200)
        assert.isTrue(response.body.result)
    })
})

describe('eth_getFilterLogs', async () => {
    it('should return an error for missing filter', async () => {
        let response = await helpers.callRPCMethod('eth_getFilterLogs', ['0xffa1'])

        assert.equal(response.status, 200)
        assert.isDefined(response.body.error)
        assert.equal(
            response.body.error.message,
            'filter by id 0xffa1 does not exist'
        )
    })

    it('should return an error for expired filter', async () => {
        let response = await helpers.callRPCMethod(
            'eth_newFilter',
            [{ 'address': contractAddress }]
        )

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        // wait for the newly created filter to expire
        // filter expiration is set to 5 seconds
        await new Promise((res) => setTimeout(() => res(), 6000))

        response = await helpers.callRPCMethod('eth_getFilterLogs', [filterID])

        assert.equal(response.status, 200)
        assert.isDefined(response.body.error)
        assert.equal(
            response.body.error.message,
            'filter by id ' + filterID + ' has expired'
        )
    })

    it('should return error message for non-logs filter', async () => {
        let response = await helpers.callRPCMethod('eth_newBlockFilter')

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        response = await helpers.callRPCMethod('eth_getFilterLogs', [filterID])

        assert.equal(response.status, 200)
        assert.equal(
            response.body.error.message,
            'invalid: filter by id ' + filterID + ' is not a logs filter'
        )
    })

    it('should return empty array when there are no logs', async () => {
        let response = await helpers.callRPCMethod(
            'eth_newFilter',
            [{ 'address': contractAddress }]
        )

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        response = await helpers.callRPCMethod('eth_getFilterLogs', [filterID])

        assert.equal(response.status, 200)
        assert.deepEqual(response.body.result, [])
    })

    it('should return matching logs from a given address log filter', async () => {
        let response = await helpers.callRPCMethod(
            'eth_newFilter',
            [{ 'address': contractAddress }]
        )

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(15, 20).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })

        assert.equal(res.receipt.status, conf.successStatus)

        response = await helpers.callRPCMethod('eth_getFilterLogs', [filterID])
        assert.equal(response.status, 200)
        assert.isUndefined(response.body.error)

        let logs = response.body.result
        assert.lengthOf(logs, 1)
        assert.equal(logs[0].address, contractAddress.toLowerCase())
        assert.lengthOf(logs[0].topics, 4)
        assert.equal(
            web3.eth.abi.decodeParameter("int256", logs[0].data),
            '35'
        )

        res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(30, 20).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })

        assert.equal(res.receipt.status, conf.successStatus)

        response = await helpers.callRPCMethod('eth_getFilterLogs', [filterID])
        assert.equal(response.status, 200)
        assert.isUndefined(response.body.error)

        logs = response.body.result
        assert.lengthOf(logs, 2)
        assert.equal(logs[1].address, contractAddress.toLowerCase())
        assert.lengthOf(logs[1].topics, 4)
        assert.equal(
            web3.eth.abi.decodeParameter("int256", logs[1].data),
            '50'
        )
    })
})

describe('eth_getFilterChanges', async () => {
    it('should return an error for missing filter', async () => {
        let response = await helpers.callRPCMethod('eth_getFilterChanges', ['0xffa1'])

        assert.equal(response.status, 200)
        assert.isDefined(response.body.error)
        assert.equal(
            response.body.error.message,
            'filter by id 0xffa1 does not exist'
        )
    })

    it('should return an error for expired filter', async () => {
        let response = await helpers.callRPCMethod(
            'eth_newFilter',
            [{ 'address': contractAddress }]
        )

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        // wait for the newly created filter to expire
        // filter expiration is set to 5 seconds
        await new Promise((res) => setTimeout(() => res(), 6000))

        response = await helpers.callRPCMethod('eth_getFilterChanges', [filterID])

        assert.equal(response.status, 200)
        assert.isDefined(response.body.error)
        assert.equal(
            response.body.error.message,
            'filter by id ' + filterID + ' has expired'
        )
    })

    it('should return empty array when there are no logs', async () => {
        let response = await helpers.callRPCMethod(
            'eth_newFilter',
            [{ 'address': contractAddress }]
        )

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        response = await helpers.callRPCMethod('eth_getFilterChanges', [filterID])

        assert.equal(response.status, 200)
        assert.deepEqual(response.body.result, [])
    })

    it('should return matching logs from a given address log filter', async () => {
        let response = await helpers.callRPCMethod(
            'eth_newFilter',
            [{ 'address': contractAddress }]
        )

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(15, 20).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })

        assert.equal(res.receipt.status, conf.successStatus)

        response = await helpers.callRPCMethod('eth_getFilterChanges', [filterID])
        assert.equal(response.status, 200)
        assert.isUndefined(response.body.error)

        let logs = response.body.result
        assert.lengthOf(logs, 1)
        assert.equal(logs[0].address, contractAddress.toLowerCase())
        assert.lengthOf(logs[0].topics, 4)
        assert.equal(
            web3.eth.abi.decodeParameter("int256", logs[0].data),
            '35'
        )

        res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(30, 20).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })

        assert.equal(res.receipt.status, conf.successStatus)

        response = await helpers.callRPCMethod('eth_getFilterChanges', [filterID])
        assert.equal(response.status, 200)
        assert.isUndefined(response.body.error)

        logs = response.body.result
        assert.lengthOf(logs, 1)
        assert.equal(logs[0].address, contractAddress.toLowerCase())
        assert.lengthOf(logs[0].topics, 4)
        assert.equal(
            web3.eth.abi.decodeParameter("int256", logs[0].data),
            '50'
        )
    })

    it('should return new blocks for a block filter', async () => {
        let response = await helpers.callRPCMethod('eth_newBlockFilter')

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(15, 20).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })

        assert.equal(res.receipt.status, conf.successStatus)

        response = await helpers.callRPCMethod('eth_getFilterChanges', [filterID])
        assert.equal(response.status, 200)
        assert.isUndefined(response.body.error)

        let blockHashes = response.body.result
        assert.lengthOf(blockHashes, 1)
        assert.equal(blockHashes[0], res.receipt.blockHash)

        res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(30, 20).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })

        assert.equal(res.receipt.status, conf.successStatus)

        response = await helpers.callRPCMethod('eth_getFilterChanges', [filterID])
        assert.equal(response.status, 200)
        assert.isUndefined(response.body.error)

        blockHashes = response.body.result
        assert.lengthOf(blockHashes, 1)
        assert.equal(blockHashes[0], res.receipt.blockHash)
    })

    it('should return new transactions for a transaction filter', async () => {
        let response = await helpers.callRPCMethod('eth_newPendingTransactionFilter')

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(15, 20).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })

        assert.equal(res.receipt.status, conf.successStatus)

        response = await helpers.callRPCMethod('eth_getFilterChanges', [filterID])
        assert.equal(response.status, 200)
        assert.isUndefined(response.body.error)

        let txHashes = response.body.result
        assert.lengthOf(txHashes, 2) // the last transaction is the COA transfer for gas fees
        assert.equal(txHashes[0], res.receipt.transactionHash)

        res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(30, 20).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })

        assert.equal(res.receipt.status, conf.successStatus)

        response = await helpers.callRPCMethod('eth_getFilterChanges', [filterID])
        assert.equal(response.status, 200)
        assert.isUndefined(response.body.error)

        txHashes = response.body.result
        assert.lengthOf(txHashes, 2) // the last transaction is the COA transfer for gas fees
        assert.equal(txHashes[0], res.receipt.transactionHash)
        assert.equal(
            txHashes[1],
            '0xb1b9deb629374d7c6df6becb7011282c8b733922b664a74ea9cd5bcb333d193e'
        )
    })

    it('should return new full transactions for a transaction filter', async () => {
        let response = await helpers.callRPCMethod('eth_newPendingTransactionFilter', [true])

        assert.equal(response.status, 200)
        let filterID = response.body.result
        assert.isDefined(filterID)

        let res = await helpers.signAndSend({
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(15, 20).encodeABI(),
            gas: 1_000_000,
            gasPrice: conf.minGasPrice
        })

        assert.equal(res.receipt.status, conf.successStatus)

        response = await helpers.callRPCMethod('eth_getFilterChanges', [filterID])
        assert.equal(response.status, 200)
        assert.isUndefined(response.body.error)

        let transactions = response.body.result
        assert.lengthOf(transactions, 2) // the last transaction is the COA transfer for gas fees
        let expectedTx = {
            blockHash: res.receipt.blockHash,
            blockNumber: '0xc',
            from: '0xFACF71692421039876a5BB4F10EF7A439D8ef61E',
            gas: '0xf4240',
            gasPrice: '0x96',
            hash: '0x0bb3c04a2bdca6c882b1d52fef8b4734d80f977aab4c023efa8f80d3115c3aae',
            input: '0x9967062d000000000000000000000000000000000000000000000000000000000000000f0000000000000000000000000000000000000000000000000000000000000014',
            nonce: '0x9',
            to: '0x99A64c993965f8d69F985b5171bC20065Cc32fAB',
            transactionIndex: '0x0',
            value: '0x0',
            type: '0x0',
            chainId: '0x286',
            v: '0x52f',
            r: '0xe599bc66adbd4d270b3683e7a57fe1f24e3ac7ec2349c10c4369ba40fed49424',
            s: '0x43983c6af62912d0fa78165d69e43eb8974130a239f9d663c262042696756f40'
        }
        assert.deepEqual(transactions[0], expectedTx)

        let expectedCoaTx = {
            blockHash: res.receipt.blockHash,
            blockNumber: '0xc',
            from: '0x0000000000000000000000030000000000000000',
            gas: '0x5b04',
            gasPrice: '0x0',
            hash: '0x71201dbf66271cedb6e87a5364b2cb84f6170e282f2b3f676196687bdf4babe0',
            input: '0x',
            nonce: '0x9',
            to: '0x658Bdf435d810C91414eC09147DAA6DB62406379',
            transactionIndex: '0x1',
            value: '0x388fb0',
            type: '0x0',
            chainId: '0x286',
            v: '0xff',
            r: '0x30000000000000000',
            s: '0x3'
        }
        assert.deepEqual(transactions[1], expectedCoaTx)
    })
})
