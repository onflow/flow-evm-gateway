const { assert } = require('chai')
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

it('should perform contract writes with relay account', async () => {
    // 1. Authorize designation of the Contract onto the EOA.
    let authorization = await walletClient.signAuthorization({
        account: eoa,
        contractAddress,
    })

    // 2. Designate the Contract on the EOA, and invoke the `initialize` function.
    let hash = await walletClient.writeContract({
        abi,
        address: eoa.address,
        authorizationList: [authorization], // 3. Pass the Authorization as a parameter.
        functionName: 'initialize',
    })

    await new Promise((res) => setTimeout(() => res(), 1500))

    let transaction = await publicClient.getTransaction({ hash: hash })
    assert.equal(transaction.from, relay.address.toLowerCase())
    assert.equal(transaction.type, 'eip7702')
    assert.equal(transaction.input, '0x8129fc1c')
    assert.deepEqual(
        transaction.authorizationList,
        [
            {
                address: '0x313af46a48eeb56d200fae0edb741628255d379f',
                chainId: 646,
                nonce: 1,
                r: '0xa79ca044cfc5754e1fcd7e0007755dc1a98894f40a2a60436fd007fe3e0298d1',
                s: '0x2897829f726104b4175474d2100d6b11d7a26498debb1917d24fae324e91275c',
                yParity: 0
            }
        ]
    )

    let txReceipt = await publicClient.getTransactionReceipt({
        hash: hash
    })
    assert.equal(txReceipt.from, relay.address.toLowerCase())
    assert.equal(txReceipt.status, 'success')
    assert.equal(txReceipt.type, 'eip7702')

    hash = await walletClient.writeContract({
        abi,
        address: eoa.address,
        functionName: 'ping',
    })

    await new Promise((res) => setTimeout(() => res(), 1500))

    transaction = await publicClient.getTransaction({ hash: hash })
    assert.equal(transaction.from, relay.address.toLowerCase())
    assert.equal(transaction.type, 'eip1559')
    assert.equal(transaction.input, '0x5c36b186')

    txReceipt = await publicClient.getTransactionReceipt({
        hash: hash
    })
    assert.equal(txReceipt.from, relay.address.toLowerCase())
    assert.equal(txReceipt.status, 'success')
    assert.equal(txReceipt.type, 'eip1559')
})

it('should perform contract writes with self-execution', async () => {
    // 1. Authorize designation of the Contract onto the EOA.
    let authorization = await walletClient.signAuthorization({
        contractAddress,
        executor: 'self',
    })

    // 2. Designate the Contract on the EOA, and invoke the `initialize` function.
    let hash = await walletClient.writeContract({
        abi,
        address: walletClient.account.address,
        authorizationList: [authorization], // 3. Pass the Authorization as a parameter.
        functionName: 'initialize',
    })

    await new Promise((res) => setTimeout(() => res(), 1500))

    let transaction = await publicClient.getTransaction({ hash: hash })
    assert.equal(transaction.from, relay.address.toLowerCase())
    assert.equal(transaction.type, 'eip7702')
    assert.equal(transaction.input, '0x8129fc1c')
    assert.deepEqual(
        transaction.authorizationList,
        [
            {
                address: '0x313af46a48eeb56d200fae0edb741628255d379f',
                chainId: 646,
                nonce: 4,
                r: '0xf4bc19cca28390f3628cfcda9076a8744b77cc87fa4f7745efa83b7a06cc3514',
                s: '0x1d736fecc68ee92ab6fd805d91a3e3dbf27097d3578561402d7105eeeee00bb7',
                yParity: 0
            }
        ]
    )

    let txReceipt = await publicClient.getTransactionReceipt({
        hash: hash
    })
    assert.equal(txReceipt.from, relay.address.toLowerCase())
    assert.equal(txReceipt.status, 'success')
    assert.equal(txReceipt.type, 'eip7702')

    hash = await walletClient.writeContract({
        abi,
        address: walletClient.account.address,
        functionName: 'ping',
    })

    await new Promise((res) => setTimeout(() => res(), 1500))

    txReceipt = await publicClient.getTransactionReceipt({
        hash: hash
    })
    assert.equal(txReceipt.from, relay.address.toLowerCase())
    assert.equal(txReceipt.status, 'success')
    assert.equal(txReceipt.type, 'eip1559')
})
