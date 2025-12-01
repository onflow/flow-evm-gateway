# Privy + wagmi Test Plan for Flow EVM Gateway

This document provides a complete test plan for building a web app using **Privy** and **wagmi** to test our custom Flow EVM Gateway with ERC-4337 support.

## Overview

**Goal**: Build a minimal web app that connects to our custom Flow EVM Gateway RPC and verifies:
- ✅ Normal EVM transactions work (EOA to EOA transfer)
- ✅ ERC-4337 UserOperations work end-to-end via our EntryPoint/bundler
- ✅ The app can be pointed at our gateway RPC (not just public Flow RPC)

**Tech Stack**:
- Next.js + TypeScript (or Vite + React + TS)
- Privy for embedded wallets & social login
- wagmi + viem for Ethereum interactions
- Custom EntryPoint and Smart Wallet contracts

---

## 1. Privy Configuration

### Step 1: Get Your Gateway RPC URL

First, determine your gateway's RPC URL:

**Option A: Public IP (if exposed)**
```
http://<EC2_PUBLIC_IP>:8545
```

**Option B: SSH Tunnel (for local testing)**
```bash
ssh -L 8545:localhost:8545 ec2-user@<EC2_PUBLIC_IP>
```
Then use: `http://localhost:8545`

### Step 2: Configure Privy Dashboard

1. Go to [Privy Dashboard](https://dashboard.privy.io/)
2. Navigate to **"Configure chain"** (or **"Networks"** → **"Custom chain"**)
3. Fill in the following values:

#### Custom Chain Configuration

| Field | Value | Notes |
|-------|-------|-------|
| **Name** | `Flow EVM Testnet` | Display name |
| **ID number** | `545` | Flow Testnet Chain ID |
| **RPC URL** | `http://<YOUR_GATEWAY_IP>:8545` | Your gateway's RPC endpoint |
| **Bundler URL** | `http://<YOUR_GATEWAY_IP>:8545` | **Same as RPC URL** - your gateway has the bundler built-in |
| **Paymaster URL** | `http://<YOUR_GATEWAY_IP>:8545` | Optional - leave empty or use your gateway URL |
| **EntryPoint Address** | `0xcf1e8398747a05a997e8c964e957e47209bdff08` | Your deployed EntryPoint v0.9.0 |
| **Smart Wallet Factory** | `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12` | Your SimpleAccountFactory |

**Important Notes**:
- ✅ **Bundler URL = RPC URL**: Your gateway implements `eth_sendUserOperation` on the same RPC endpoint
- ✅ **Custom EntryPoint**: Privy supports custom EntryPoint addresses - enter yours in the "EntryPoint Address" field
- ✅ **Custom Factory**: Privy supports custom smart wallet factories - enter your SimpleAccountFactory address
- ⚠️ **Warning**: Privy will show a warning "Make sure the smart wallet contract is deployed on this chain" - this is expected and safe to ignore if your contracts are deployed

### Step 3: Quick Setup vs Custom Chain

**For this test, use "Custom chain"** (not "Quick setup"):
- Quick setup is for pre-configured chains (Ethereum, Polygon, etc.)
- Custom chain allows you to specify your own EntryPoint and factory addresses
- Custom chain is required to use your deployed contracts

---

## 2. Contract Addresses Reference

All addresses are on **Flow Testnet (Chain ID: 545)**:

| Contract | Address | Explorer |
|----------|---------|----------|
| **EntryPoint (v0.9.0)** | `0xcf1e8398747a05a997e8c964e957e47209bdff08` | [View on Flowscan](https://evm-testnet.flowscan.io/address/0xcf1e8398747a05a997e8c964e957e47209bdff08) |
| **SimpleAccountFactory** | `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12` | [View on Flowscan](https://evm-testnet.flowscan.io/address/0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12) |
| **PaymasterERC20** (optional) | `0x486a2c4BC557914ee83B8fCcc4bAae11FdA70B2a` | [View on Flowscan](https://evm-testnet.flowscan.io/address/0x486a2c4BC557914ee83B8fCcc4bAae11FdA70B2a) |
| **TestToken** (optional) | `0x99C7A1c5eCf02d3Dd01D2B7F5936D6611E8473CD` | [View on Flowscan](https://evm-testnet.flowscan.io/address/0x99C7A1c5eCf02d3Dd01D2B7F5936D6611E8473CD) |

**Block Explorer**: https://evm-testnet.flowscan.io

---

## 3. App Setup

### 3.1 Project Structure

```
privy-gateway-test/
├── .env.local                 # Privy app ID, RPC URLs
├── package.json
├── tsconfig.json
├── next.config.js
├── src/
│   ├── app/
│   │   ├── layout.tsx
│   │   └── page.tsx
│   ├── components/
│   │   ├── ConnectView.tsx
│   │   ├── Dashboard.tsx
│   │   ├── SendTransaction.tsx
│   │   └── SendUserOp.tsx
│   ├── config/
│   │   └── flowTestnetEvmGateway.ts
│   └── providers/
│       └── AppProviders.tsx
└── README.md
```

### 3.2 Dependencies

```json
{
  "dependencies": {
    "@privy-io/react-auth": "^2.x.x",
    "wagmi": "^2.x.x",
    "viem": "^2.x.x",
    "@tanstack/react-query": "^5.x.x",
    "next": "^14.x.x",
    "react": "^18.x.x",
    "react-dom": "^18.x.x"
  },
  "devDependencies": {
    "@types/node": "^20.x.x",
    "@types/react": "^18.x.x",
    "typescript": "^5.x.x"
  }
}
```

### 3.3 Environment Variables (`.env.local`)

```bash
# Privy Configuration
NEXT_PUBLIC_PRIVY_APP_ID=your-privy-app-id

# Gateway RPC URL (use your EC2 public IP or localhost via SSH tunnel)
NEXT_PUBLIC_GATEWAY_RPC_URL=http://<YOUR_GATEWAY_IP>:8545

# Public Flow RPC (for comparison)
NEXT_PUBLIC_PUBLIC_RPC_URL=https://testnet.evm.nodes.onflow.org

# Chain ID
NEXT_PUBLIC_CHAIN_ID=545

# Contract Addresses
NEXT_PUBLIC_ENTRY_POINT_ADDRESS=0xcf1e8398747a05a997e8c964e957e47209bdff08
NEXT_PUBLIC_FACTORY_ADDRESS=0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12
```

---

## 4. Configuration Files

### 4.1 Chain Configuration (`src/config/flowTestnetEvmGateway.ts`)

```typescript
import { defineChain } from 'viem'

export const flowTestnetEvmGateway = defineChain({
  id: 545,
  name: 'Flow EVM Testnet (Gateway)',
  nativeCurrency: {
    name: 'FLOW',
    symbol: 'FLOW',
    decimals: 18,
  },
  rpcUrls: {
    default: {
      http: [process.env.NEXT_PUBLIC_GATEWAY_RPC_URL || 'http://localhost:8545'],
    },
  },
  blockExplorers: {
    default: {
      name: 'Flowscan',
      url: 'https://evm-testnet.flowscan.io',
    },
  },
  contracts: {
    entryPoint: {
      address: '0xcf1e8398747a05a997e8c964e957e47209bdff08',
    },
  },
})

export const flowTestnetPublic = defineChain({
  id: 545,
  name: 'Flow EVM Testnet (Public)',
  nativeCurrency: {
    name: 'FLOW',
    symbol: 'FLOW',
    decimals: 18,
  },
  rpcUrls: {
    default: {
      http: ['https://testnet.evm.nodes.onflow.org'],
    },
  },
  blockExplorers: {
    default: {
      name: 'Flowscan',
      url: 'https://evm-testnet.flowscan.io',
    },
  },
})
```

### 4.2 App Providers (`src/providers/AppProviders.tsx`)

```typescript
'use client'

import { PrivyProvider } from '@privy-io/react-auth'
import { WagmiProvider } from 'wagmi'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createConfig, http } from 'wagmi'
import { flowTestnetEvmGateway } from '@/config/flowTestnetEvmGateway'

const queryClient = new QueryClient()

const wagmiConfig = createConfig({
  chains: [flowTestnetEvmGateway],
  transports: {
    [flowTestnetEvmGateway.id]: http(process.env.NEXT_PUBLIC_GATEWAY_RPC_URL),
  },
})

export function AppProviders({ children }: { children: React.ReactNode }) {
  return (
    <PrivyProvider
      appId={process.env.NEXT_PUBLIC_PRIVY_APP_ID!}
      config={{
        loginMethods: ['email', 'wallet'],
        appearance: {
          theme: 'light',
          accentColor: '#676FFF',
        },
        embeddedWallets: {
          createOnLogin: 'users-without-wallets',
        },
      }}
    >
      <WagmiProvider config={wagmiConfig}>
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      </WagmiProvider>
    </PrivyProvider>
  )
}
```

**Important**: Privy will automatically use the EntryPoint and factory addresses you configured in the Privy Dashboard. The `PrivyProvider` reads these from your Privy app configuration.

---

## 5. Test Cases

### Test Case 1: Connect & Verify Gateway Connection

**Component**: `ConnectView.tsx`

**Steps**:
1. User clicks "Connect with Privy"
2. User authenticates (email or social login)
3. App displays:
   - Connected EOA address
   - Current chain ID (should be `545`)
   - Current block number (from gateway)

**Validation**:
- ✅ Block number updates in real-time
- ✅ Block number matches gateway's current block (check via `eth_blockNumber`)

**Code Example**:
```typescript
'use client'

import { usePrivy } from '@privy-io/react-auth'
import { useAccount, useBlockNumber, useChainId } from 'wagmi'

export function ConnectView() {
  const { ready, authenticated, login, user } = usePrivy()
  const { address } = useAccount()
  const { data: blockNumber } = useBlockNumber()
  const chainId = useChainId()

  if (!ready) return <div>Loading...</div>
  if (!authenticated) {
    return <button onClick={login}>Connect with Privy</button>
  }

  return (
    <div>
      <h2>Connected</h2>
      <p>Address: {address}</p>
      <p>Chain ID: {chainId}</p>
      <p>Block Number: {blockNumber?.toString()}</p>
    </div>
  )
}
```

---

### Test Case 2: Send Normal EVM Transaction

**Component**: `SendTransaction.tsx`

**Goal**: Verify normal `eth_sendRawTransaction` works against our gateway.

**Steps**:
1. User enters recipient address and amount (in FLOW)
2. User clicks "Send Transaction"
3. Privy prompts for signature
4. Transaction is sent via wagmi to gateway
5. App displays transaction hash and link to Flowscan

**Validation**:
- ✅ Transaction appears on Flowscan
- ✅ Balance updates correctly
- ✅ Transaction is processed by gateway (check gateway logs)

**Code Example**:
```typescript
'use client'

import { useSendTransaction, useWaitForTransactionReceipt } from 'wagmi'
import { parseEther } from 'viem'
import { useState } from 'react'

export function SendTransaction() {
  const [recipient, setRecipient] = useState('')
  const [amount, setAmount] = useState('0.0001')

  const { data: hash, sendTransaction, isPending } = useSendTransaction()
  const { isLoading: isConfirming, isSuccess } = useWaitForTransactionReceipt({
    hash,
  })

  const handleSend = () => {
    if (!recipient) return
    sendTransaction({
      to: recipient as `0x${string}`,
      value: parseEther(amount),
    })
  }

  return (
    <div>
      <h2>Send Transaction</h2>
      <input
        type="text"
        placeholder="Recipient address"
        value={recipient}
        onChange={(e) => setRecipient(e.target.value)}
      />
      <input
        type="text"
        placeholder="Amount (FLOW)"
        value={amount}
        onChange={(e) => setAmount(e.target.value)}
      />
      <button onClick={handleSend} disabled={isPending}>
        {isPending ? 'Sending...' : 'Send'}
      </button>
      {hash && (
        <div>
          <p>Tx Hash: {hash}</p>
          <a
            href={`https://evm-testnet.flowscan.io/tx/${hash}`}
            target="_blank"
            rel="noopener noreferrer"
          >
            View on Flowscan
          </a>
        </div>
      )}
      {isSuccess && <p>✅ Transaction confirmed!</p>}
    </div>
  )
}
```

---

### Test Case 3: Send ERC-4337 UserOperation

**Component**: `SendUserOp.tsx`

**Goal**: Submit a UserOperation through EntryPoint using our gateway's bundler.

**Important**: Privy handles UserOperation creation automatically when using smart wallets. However, you can also manually create UserOperations if needed.

**Steps**:
1. User connects with Privy (creates embedded wallet)
2. Privy automatically creates a smart wallet (SimpleAccount) if needed
3. User sends a transaction via smart wallet
4. Privy creates UserOperation and sends to gateway's `eth_sendUserOperation`
5. Gateway bundles and submits to EntryPoint
6. App displays UserOp hash and final transaction hash

**Validation**:
- ✅ UserOperation is accepted by gateway (returns hash)
- ✅ Final transaction appears on Flowscan with `to = EntryPoint`
- ✅ Transaction logs show `UserOperationEvent`
- ✅ Smart wallet balance changes correctly

**Code Example** (using Privy's built-in smart wallet):
```typescript
'use client'

import { usePrivy, useWallets } from '@privy-io/react-auth'
import { useSendTransaction } from 'wagmi'
import { parseEther } from 'viem'
import { useState } from 'react'

export function SendUserOp() {
  const { user } = usePrivy()
  const { wallets } = useWallets()
  const [recipient, setRecipient] = useState('')
  const [amount, setAmount] = useState('0.0001')

  // Find the smart wallet (embedded wallet)
  const smartWallet = wallets.find((w) => w.walletClientType === 'privy')

  const { data: hash, sendTransaction, isPending } = useSendTransaction({
    account: smartWallet?.address as `0x${string}`,
  })

  const handleSend = () => {
    if (!recipient || !smartWallet) return
    sendTransaction({
      to: recipient as `0x${string}`,
      value: parseEther(amount),
    })
  }

  return (
    <div>
      <h2>Send UserOperation (Smart Wallet)</h2>
      {smartWallet && (
        <p>Smart Wallet: {smartWallet.address}</p>
      )}
      <input
        type="text"
        placeholder="Recipient address"
        value={recipient}
        onChange={(e) => setRecipient(e.target.value)}
      />
      <input
        type="text"
        placeholder="Amount (FLOW)"
        value={amount}
        onChange={(e) => setAmount(e.target.value)}
      />
      <button onClick={handleSend} disabled={isPending || !smartWallet}>
        {isPending ? 'Sending...' : 'Send UserOp'}
      </button>
      {hash && (
        <div>
          <p>Tx Hash: {hash}</p>
          <a
            href={`https://evm-testnet.flowscan.io/tx/${hash}`}
            target="_blank"
            rel="noopener noreferrer"
          >
            View on Flowscan
          </a>
        </div>
      )}
    </div>
  )
}
```

**Note**: Privy automatically:
- Creates UserOperations when using smart wallets
- Sends them to your configured bundler URL (your gateway)
- Uses your configured EntryPoint and factory addresses

---

### Test Case 4: Compare Gateway vs Public RPC

**Component**: `Dashboard.tsx`

**Goal**: Verify both RPCs return similar block heights.

**Steps**:
1. Add toggle: "RPC: Gateway / Public"
2. When toggled, recreate wagmi config with different RPC
3. Display block numbers from both RPCs side-by-side

**Validation**:
- ✅ Both RPCs return block numbers within a few blocks of each other
- ✅ App continues working when switching RPCs

**Code Example**:
```typescript
'use client'

import { useBlockNumber } from 'wagmi'
import { useState } from 'react'
import { createPublicClient, http } from 'viem'
import { flowTestnetPublic } from '@/config/flowTestnetEvmGateway'

export function Dashboard() {
  const { data: gatewayBlock } = useBlockNumber()
  const [publicBlock, setPublicBlock] = useState<bigint | null>(null)

  const checkPublicBlock = async () => {
    const publicClient = createPublicClient({
      chain: flowTestnetPublic,
      transport: http('https://testnet.evm.nodes.onflow.org'),
    })
    const block = await publicClient.getBlockNumber()
    setPublicBlock(block)
  }

  return (
    <div>
      <h2>Block Comparison</h2>
      <p>Gateway Block: {gatewayBlock?.toString()}</p>
      <button onClick={checkPublicBlock}>Check Public Block</button>
      {publicBlock && <p>Public Block: {publicBlock.toString()}</p>}
      {gatewayBlock && publicBlock && (
        <p>
          Difference: {Number(gatewayBlock - publicBlock)} blocks
        </p>
      )}
    </div>
  )
}
```

---

## 6. Testing Checklist

### Pre-Testing Setup
- [ ] Privy app created and configured with custom chain
- [ ] Gateway RPC URL accessible (public IP or SSH tunnel)
- [ ] Gateway service running and synced
- [ ] `.env.local` configured with Privy app ID and RPC URL

### Test Case 1: Connection
- [ ] User can connect with Privy (email/social)
- [ ] Connected address displays correctly
- [ ] Chain ID is `545`
- [ ] Block number updates in real-time
- [ ] Block number matches gateway's current block

### Test Case 2: Normal Transaction
- [ ] Transaction form accepts recipient and amount
- [ ] Transaction is sent successfully
- [ ] Transaction hash is displayed
- [ ] Transaction appears on Flowscan
- [ ] Balance updates correctly

### Test Case 3: UserOperation
- [ ] Smart wallet is created automatically (or manually)
- [ ] UserOperation is sent successfully
- [ ] UserOp hash is returned
- [ ] Final transaction appears on Flowscan
- [ ] Transaction logs show `UserOperationEvent`
- [ ] Smart wallet balance changes correctly

### Test Case 4: RPC Comparison
- [ ] Toggle between Gateway and Public RPC works
- [ ] Both RPCs return block numbers
- [ ] Block numbers are within reasonable range (few blocks difference)
- [ ] App continues working when switching RPCs

---

## 7. Troubleshooting

### Issue: "Smart wallet contract not deployed" warning in Privy Dashboard
**Solution**: This is expected if you're using custom contracts. Verify your contracts are deployed:
- EntryPoint: https://evm-testnet.flowscan.io/address/0xcf1e8398747a05a997e8c964e957e47209bdff08
- Factory: https://evm-testnet.flowscan.io/address/0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12

### Issue: "Connection refused" when connecting to gateway
**Solution**: 
- Check gateway service is running: `sudo systemctl status flow-evm-gateway`
- Check gateway is listening on port 8545: `docker ps` and check port mapping
- If using SSH tunnel, ensure tunnel is active: `ssh -L 8545:localhost:8545 ec2-user@<IP>`

### Issue: UserOperations not being accepted
**Solution**:
- Verify gateway has `BUNDLER_ENABLED=true` in config
- Check gateway logs: `sudo journalctl -u flow-evm-gateway -f`
- Verify EntryPoint address matches in Privy config and gateway config
- Check gateway is synced: `eth_syncing` should return `false`

### Issue: Block numbers don't match between Gateway and Public RPC
**Solution**:
- Gateway may be slightly behind if still syncing
- Check gateway sync status: `eth_syncing`
- Wait for gateway to catch up (should be within a few blocks)

---

## 8. Deliverables

After completing the test plan, provide:

1. **GitHub Repository** with:
   - Complete Next.js app code
   - `README.md` with setup instructions
   - `.env.example` template
   - Screenshots of working tests

2. **Test Results**:
   - Screenshot of Test Case 1 (connection)
   - Screenshot of Test Case 2 (normal transaction on Flowscan)
   - Screenshot of Test Case 3 (UserOperation transaction on Flowscan)
   - Screenshot of Test Case 4 (block comparison)

3. **Notes**:
   - Any issues encountered
   - Performance observations
   - Recommendations for improvements

---

## 9. Quick Reference

### Gateway RPC Endpoint
```
http://<EC2_PUBLIC_IP>:8545
```

### Contract Addresses
- **EntryPoint**: `0xcf1e8398747a05a997e8c964e957e47209bdff08`
- **Factory**: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12`
- **Paymaster** (optional): `0x486a2c4BC557914ee83B8fCcc4bAae11FdA70B2a`

### Block Explorer
```
https://evm-testnet.flowscan.io
```

### Chain ID
```
545
```

---

## 10. Next Steps

1. **Create Privy App**: Go to https://dashboard.privy.io/ and create a new app
2. **Configure Custom Chain**: Use the values from Section 1
3. **Get Privy App ID**: Copy your app ID to `.env.local`
4. **Build App**: Follow the structure in Section 3
5. **Test**: Run through all test cases in Section 5
6. **Verify**: Check transactions on Flowscan and gateway logs

---

## Support

- **Privy Docs**: https://docs.privy.io/
- **wagmi Docs**: https://wagmi.sh/
- **Flow EVM Gateway**: See `docs/AWS_DEPLOYMENT.md` for deployment details
- **Contract Addresses**: See `docs/FLOW_TESTNET_DEPLOYMENT.md`


