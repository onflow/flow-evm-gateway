const { assert } = require('chai')
const { encodeFunctionData } = require('viem')
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

it('should send transactions with relay account', async () => {
    // 1. Authorize designation of the Contract onto the EOA.
    let authorization = await walletClient.signAuthorization({
        account: eoa,
        contractAddress,
    })

    // 2. Designate the Contract on the EOA, and invoke the `initialize` function.
    let hash = await walletClient.sendTransaction({
        authorizationList: [authorization], // 3. Pass the Authorization as a parameter.
        data: encodeFunctionData({
            abi,
            functionName: 'initialize',
        }),
        to: eoa.address,
    })

    await new Promise((res) => setTimeout(() => res(), 1500))
    let transaction = await publicClient.getTransactionReceipt({
        hash: hash
    })
    assert.equal(transaction.status, 'success')
    assert.equal(transaction.type, 'eip7702')

    hash = await walletClient.sendTransaction({
        data: encodeFunctionData({
            abi,
            functionName: 'ping',
        }),
        to: eoa.address,
    })

    await new Promise((res) => setTimeout(() => res(), 1500))
    transaction = await publicClient.getTransactionReceipt({
        hash: hash
    })
    assert.equal(transaction.status, 'success')
    assert.equal(transaction.type, 'eip1559')
})

it('should send self-executing transactions', async () => {
    // 1. Authorize designation of the Contract onto the EOA.
    let authorization = await walletClient.signAuthorization({
        contractAddress,
        executor: 'self',
    })

    // 2. Designate the Contract on the EOA, and invoke the `initialize` function.
    let hash = await walletClient.sendTransaction({
        authorizationList: [authorization], // 3. Pass the Authorization as a parameter.
        data: encodeFunctionData({
            abi,
            functionName: 'initialize',
        }),
        to: walletClient.account.address,
    })

    await new Promise((res) => setTimeout(() => res(), 1500))
    let transaction = await publicClient.getTransactionReceipt({
        hash: hash
    })
    assert.equal(transaction.status, 'success')
    assert.equal(transaction.type, 'eip7702')

    hash = await walletClient.sendTransaction({
        data: encodeFunctionData({
            abi,
            functionName: 'ping',
        }),
        to: walletClient.account.address,
    })

    await new Promise((res) => setTimeout(() => res(), 1500))
    transaction = await publicClient.getTransactionReceipt({
        hash: hash
    })
    assert.equal(transaction.status, 'success')
    assert.equal(transaction.type, 'eip1559')
})
