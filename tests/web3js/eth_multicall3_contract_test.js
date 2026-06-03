const fs = require('fs')
const utils = require('web3-utils')
const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

// Address that will deploy the multicall3 contract (must have been pre-funded)
const MULTICALL3_DEPLOYER = '0x05f32B3cC3888453ff71B01135B34FF8e41263F2'

it('deploys multicall3 contract and interacts', async () => {
    let deployed = await helpers.deployContract('storage')
    let contractAddress = deployed.receipt.contractAddress

    // get the default deployed value on contract
    const initValue = 1337
    let callRetrieve = deployed.contract.methods.retrieve().encodeABI()
    result = await web3.eth.call({ to: contractAddress, data: callRetrieve }, 'latest')
    assert.equal(result, initValue)

    // Fund the multicall3 deployer address
    let transfer = await helpers.signAndSend({
        from: conf.eoa.address,
        to: MULTICALL3_DEPLOYER,
        value: utils.toWei('1.0', 'ether'),
        gasPrice: conf.minGasPrice,
        gasLimit: 55_000,
    })
    assert.equal(transfer.receipt.status, conf.successStatus)

    // Deploy multicall3 using pre-signed raw transaction
    // Note: The transaction in multicall3.byte must be signed
    // by the address defined in the `MULTICALL3_DEPLOYER` const
    let multicall3DeploymentTx = await fs.promises.readFile(`${__dirname}/../fixtures/multicall3.byte`, 'utf8')
    let response = await helpers.callRPCMethod(
        'eth_sendRawTransaction',
        [multicall3DeploymentTx]
    )
    assert.equal(200, response.status)
    assert.isDefined(response.body.result, 'Transaction hash should be returned')

    let txHash = response.body.result

    // wait for the transaction receipt to become available
    await new Promise((res, _) => setTimeout(() => res(), 1500))
    let receipt = await web3.eth.getTransactionReceipt(txHash)
    let multicall3Address = receipt.contractAddress

    // make sure deploy results are correct
    assert.equal(receipt.status, conf.successStatus)
    assert.equal(receipt.from, MULTICALL3_DEPLOYER.toLowerCase())
    assert.isString(receipt.transactionHash)
    assert.isString(multicall3Address)

    let tx = await web3.eth.getTransaction(txHash)
    assert.equal(tx.from, MULTICALL3_DEPLOYER.toLowerCase())
    assert.isUndefined(tx.chainId)

    let block = await web3.eth.getBlock(receipt.blockNumber, true)
    assert.equal(block.transactions[0].from, MULTICALL3_DEPLOYER.toLowerCase())
    assert.isUndefined(block.transactions[0].chainId)

    let multicall3ABI = require('../fixtures/multicall3ABI.json')
    let multicall3 = new web3.eth.Contract(multicall3ABI, multicall3Address, { handleReverted: true })

    let callSum20 = deployed.contract.methods.sum(10, 10).encodeABI()
    let callSum50 = deployed.contract.methods.sum(10, 40).encodeABI()
    let callAggregate3 = multicall3.methods.aggregate3(
        [
            {
                target: contractAddress,
                allowFailure: false,
                callData: callSum20
            },
            {
                target: contractAddress,
                allowFailure: false,
                callData: callSum50
            }
        ]
    ).encodeABI()

    result = await web3.eth.call(
        {
            to: multicall3Address,
            data: callAggregate3
        },
        'latest'
    )
    let decodedResult = web3.eth.abi.decodeParameter(
        {
            'Result[]': {
                'success': 'bool',
                'returnData': 'bytes'
            }
        },
        result
    )

    assert.lengthOf(decodedResult, 2)

    assert.equal(decodedResult[0].success, true)
    assert.equal(decodedResult[0].returnData, 20n)

    assert.equal(decodedResult[1].success, true)
    assert.equal(decodedResult[1].returnData, 50n)
})
