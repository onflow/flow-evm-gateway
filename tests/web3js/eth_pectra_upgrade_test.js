const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

const { privateKeyToAccount } = require('viem/accounts')
const { relay, walletClient, publicClient } = require('./viem/config')
const { abi, bytecode } = require('./viem/contract')

// eoa is 0xfe847d8bebe46799FCE83eB52f38Ef4b907996A6
const eoa = privateKeyToAccount('0x3a0901a19a40f2041727fe1a973137ad917fc925ce716983e1376e927658b12e')
let contractAddress = null

before(async () => {
    let request = await walletClient.prepareTransactionRequest({
        relay,
        to: eoa.address,
        value: 1000000000000000000n
    })
    let serializedTransaction = await walletClient.signTransaction(request)

    let hash = await walletClient.sendRawTransaction({ serializedTransaction })

    await new Promise((res) => setTimeout(() => res(), 1500))
    let transaction = await publicClient.getTransactionReceipt({
        hash: hash
    })
    assert.equal(transaction.status, 'success')

    hash = await walletClient.deployContract({
        abi: abi,
        account: eoa,
        bytecode: bytecode,
    })

    await new Promise((res) => setTimeout(() => res(), 1500))
    transaction = await publicClient.getTransactionReceipt({
        hash: hash
    })
    assert.equal(transaction.status, 'success')

    contractAddress = transaction.contractAddress
})

it('should not perform contract writes with relay account before Pectra', async () => {
    // 1. Authorize designation of the Contract onto the EOA.
    let authorization = await walletClient.signAuthorization({
        account: eoa,
        contractAddress,
    })

    let errMsg = ''
    try {
        // 2. Designate the Contract on the EOA, and invoke the `initialize` function.
        await walletClient.writeContract({
            abi,
            address: eoa.address,
            authorizationList: [authorization], // 3. Pass the Authorization as a parameter.
            functionName: 'initialize',
        })
    } catch (e) {
        errMsg = e.details
    }

    assert.equal(
        errMsg,
        'transaction type not supported: type 4 rejected, pool not yet in Prague'
    )
})

it('should verify gas estimation and debug tracing functionality before Pectra', async () => {
    let deployed = await helpers.deployContract('storage')
    let contractAddress = deployed.receipt.contractAddress

    let gasEstimate = await web3.eth.estimateGas(
        {
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(100, 200).encodeABI(),
            gas: 55_000,
            gasPrice: conf.minGasPrice
        },
        'earliest'
    )
    assert.equal(gasEstimate, 21510n)

    let storeData = deployed.contract.methods.store(0).encodeABI()
    gasEstimate = await web3.eth.estimateGas(
        {
            from: conf.eoa.address,
            to: contractAddress,
            data: storeData,
            gas: 55_000,
            gasPrice: conf.minGasPrice
        },
        'earliest'
    )
    assert.equal(gasEstimate, 21358n)

    // submit a transaction that calls verifyArchCallToFlowBlockHeight()
    let flowBlockHeightData = deployed.contract.methods.verifyArchCallToFlowBlockHeight().encodeABI()
    res = await helpers.signAndSend({
        from: conf.eoa.address,
        to: contractAddress,
        data: flowBlockHeightData,
        value: '0',
        gasPrice: conf.minGasPrice,
    })
    assert.equal(res.receipt.status, conf.successStatus)

    let callTracer = {
        tracer: 'callTracer',
        tracerConfig: {
            onlyTopCall: false
        }
    }
    let response = await helpers.callRPCMethod(
        'debug_traceTransaction',
        [web3.utils.toHex(res.receipt.transactionHash), callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body.result)

    let txTrace = response.body.result

    assert.deepEqual(
        txTrace,
        {
            from: conf.eoa.address.toLowerCase(),
            gas: '0xb50d',
            gasUsed: '0x6147',
            to: contractAddress.toLowerCase(),
            input: '0xc550f90f',
            output: '0x0000000000000000000000000000000000000000000000000000000000000007',
            calls: [
                {
                    from: contractAddress.toLowerCase(),
                    gas: '0x54e1',
                    gasUsed: '0x2',
                    to: '0x0000000000000000000000010000000000000001',
                    input: '0x53e87d66',
                    output: '0x0000000000000000000000000000000000000000000000000000000000000007',
                    type: 'STATICCALL'
                }
            ],
            value: '0x0',
            type: 'CALL'
        }
    )

    callTracer = {
        tracer: 'callTracer',
        tracerConfig: {
            withLog: false,
            onlyTopCall: false
        }
    }

    flowBlockHeightData = deployed.contract.methods.verifyArchCallToFlowBlockHeight().encodeABI()
    let traceCall = {
        from: conf.eoa.address,
        to: contractAddress,
        gas: '0xcdd4',
        data: flowBlockHeightData,
        value: '0x0',
        gasPrice: web3.utils.toHex(conf.minGasPrice),
    }

    response = await helpers.callRPCMethod(
        'debug_traceCall',
        [traceCall, 'latest', callTracer]
    )
    assert.equal(response.status, 200)
    assert.isDefined(response.body)

    let callTrace = response.body.result
    assert.deepEqual(
        callTrace,
        {
            from: conf.eoa.address.toLowerCase(),
            gas: '0xcdd4',
            gasUsed: '0xb38f',
            to: contractAddress.toLowerCase(),
            input: '0xc550f90f',
            output: '0x0000000000000000000000000000000000000000000000000000000000000007',
            calls: [
                {
                    from: contractAddress.toLowerCase(),
                    gas: '0x6d44',
                    gasUsed: '0x524a',
                    to: '0x0000000000000000000000010000000000000001',
                    input: '0x53e87d66',
                    output: '0x0000000000000000000000000000000000000000000000000000000000000007',
                    type: 'STATICCALL'
                }
            ],
            value: '0x0',
            type: 'CALL'
        }
    )
})
