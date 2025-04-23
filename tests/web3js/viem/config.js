const { createWalletClient, createPublicClient, http } = require('viem')
const { defineChain } = require('viem')
const { privateKeyToAccount } = require('viem/accounts')

const flowLocal = defineChain({
    id: 646,
    name: 'Flow EVM Local',
    nativeCurrency: {
        decimals: 18,
        name: 'Flow',
        symbol: 'FLOW',
    },
    rpcUrls: {
        default: {
            http: ['http://localhost:8545'],
        },
    },
    blockExplorers: {},
    contracts: {},
    testnet: true,
})

// Address: 0xfacf71692421039876a5bb4f10ef7a439d8ef61e
const relay = privateKeyToAccount('0xf6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442')

const walletClient = createWalletClient({
    account: relay,
    chain: flowLocal,
    transport: http(),
})

const publicClient = createPublicClient({
    chain: flowLocal,
    transport: http()
})

exports.relay = relay
exports.walletClient = walletClient
exports.publicClient = publicClient
