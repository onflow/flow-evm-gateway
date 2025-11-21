# ERC-4337 (Account Abstraction) Support Plan for EVM Gateway

## Executive Summary

This document outlines a comprehensive plan for adding ERC-4337 (Account Abstraction) support to the Flow EVM Gateway. The gateway will function as a bundler, receiving UserOperations from wallets, validating them, batching them, and wrapping the resulting `EntryPoint.handleOps()` transactions in Cadence transactions (just like normal EVM transactions).

## Current Architecture Analysis

### Key Components Identified

1. **API Layer** (`api/api.go`):

   - Handles JSON-RPC requests
   - Currently supports `eth_sendRawTransaction` and other standard methods
   - Uses rate limiting and validation

2. **Transaction Pool** (`services/requester/tx_pool.go`, `single_tx_pool.go`, `batch_tx_pool.go`):

   - `SingleTxPool`: Submits transactions immediately
   - `BatchTxPool`: Groups transactions by EOA address and batches them
   - Both wrap EVM transactions in Cadence transactions using `run.cdc` script
   - **Key Feature**: Can batch multiple EVM transactions into a single Cadence transaction via `EVM.batchRun()`

3. **Cadence Wrapping** (`services/requester/cadence/run.cdc`):

   - Takes hex-encoded EVM transactions as parameters (array)
   - Calls `EVM.run()` for single transactions or `EVM.batchRun()` for multiple
   - **This is the gateway's unique advantage**: Can execute multiple EVM transactions atomically in one Cadence transaction
   - Handles errors and validation

4. **Simulation/Validation** (`services/requester/requester.go`):

   - `dryRunTx()` method for simulating transactions without state changes
   - Used by `eth_call` and `eth_estimateGas`
   - Supports state overrides

5. **Configuration** (`config/config.go`):
   - Centralized config structure
   - Supports transaction batching, rate limiting, gas price enforcement

### Gateway's Unique Architecture Advantage

**Traditional EVM Networks**:

- Each transaction is separate on-chain
- Bundlers create one `EntryPoint.handleOps([...all userOps...])` transaction
- Limited by single transaction gas limits

**Flow EVM Gateway**:

- Multiple EVM transactions can be batched into **one Cadence transaction**
- Can create multiple `EntryPoint.handleOps()` transactions and batch them together
- More efficient: Single Cadence overhead for multiple EntryPoint calls
- Better atomicity: All UserOps in the batch succeed or fail together

## ERC-4337 Requirements

### 1. New JSON-RPC Methods

The gateway needs to implement the standard ERC-4337 bundler RPC methods:

#### `eth_sendUserOperation`

- **Purpose**: Receive UserOperations from wallets
- **Parameters**:
  - `UserOperation` object
  - `EntryPoint` address (optional, can use configured default)
- **Returns**: `userOperationHash` (keccak256 hash of the UserOperation)
- **Location**: Add to `api/api.go` as new method in `BlockChainAPI`

#### `eth_estimateUserOperationGas`

- **Purpose**: Estimate gas costs for a UserOperation before submission
- **Parameters**: Same as `eth_sendUserOperation`
- **Returns**: Gas estimates (preVerificationGas, verificationGas, callGasLimit)
- **Location**: Add to `api/api.go`

#### `eth_getUserOperationByHash`

- **Purpose**: Retrieve UserOperation details by hash
- **Parameters**: `userOperationHash`
- **Returns**: UserOperation object with status
- **Location**: Add to `api/api.go`

#### `eth_getUserOperationReceipt`

- **Purpose**: Get receipt for a UserOperation (similar to transaction receipt)
- **Parameters**: `userOperationHash`
- **Returns**: UserOperation receipt with execution details
- **Location**: Add to `api/api.go`

### 2. UserOperation Data Structures

**New File**: `models/user_operation.go`

```go
type UserOperation struct {
    Sender               common.Address
    Nonce               *big.Int
    InitCode            []byte
    CallData            []byte
    CallGasLimit        *big.Int
    VerificationGasLimit *big.Int
    PreVerificationGas  *big.Int
    MaxFeePerGas        *big.Int
    MaxPriorityFeePerGas *big.Int
    PaymasterAndData    []byte
    Signature           []byte
}

// Hash computes the UserOperation hash per ERC-4337 spec
func (uo *UserOperation) Hash(entryPoint common.Address, chainID *big.Int) common.Hash

// PackForSignature packs the UserOperation for signature verification
func (uo *UserOperation) PackForSignature(entryPoint common.Address, chainID *big.Int) []byte
```

### 3. UserOperation Mempool (Alt-Mempool)

**New File**: `services/requester/userop_pool.go`

The UserOperation pool is separate from the regular transaction pool:

```go
type UserOperationPool interface {
    Add(ctx context.Context, userOp *models.UserOperation, entryPoint common.Address) (common.Hash, error)
    GetByHash(hash common.Hash) (*models.UserOperation, error)
    GetPending() []*models.UserOperation
    Remove(hash common.Hash)
}

type InMemoryUserOpPool struct {
    // Storage for pending UserOperations
    // Grouped by sender address for nonce ordering
    // TTL for stale operations
    // Duplicate detection
}
```

**Key Features**:

- Store UserOperations keyed by `(sender, nonce)` to prevent duplicates
- Track UserOperation hashes for lookup
- Implement TTL for stale operations
- Group by sender address for efficient batching
- Thread-safe operations

### 4. EntryPoint Contract Integration

**Configuration Addition** (`config/config.go`):

```go
type Config struct {
    // ... existing fields ...

    // ERC-4337 Configuration
    EntryPointAddress common.Address  // Canonical EntryPoint contract address
    BundlerEnabled    bool            // Enable bundler functionality
    MaxOpsPerBundle   int             // Maximum UserOps per handleOps call
    UserOpTTL         time.Duration   // Time to live for pending UserOps
}
```

**EntryPoint ABI**: Need to import or define the EntryPoint contract ABI for:

- `handleOps(UserOperation[] ops, address payable beneficiary)`
- `simulateValidation(UserOperation calldata userOp)`
- `getUserOpHash(UserOperation calldata userOp)`

### 5. UserOperation Validation & Simulation

**New File**: `services/requester/userop_validator.go`

```go
type UserOpValidator struct {
    client        *CrossSporkClient
    config        config.Config
    blockView     // For state access
}

func (v *UserOpValidator) Validate(ctx context.Context, userOp *models.UserOperation, entryPoint common.Address) error {
    // 1. Basic validation (signature, nonce, gas limits)
    // 2. Call EntryPoint.simulateValidation via eth_call
    // 3. Check paymaster deposit (if paymaster present)
    // 4. Verify account exists or can be deployed
    // 5. Check gas price/fee requirements
}
```

**Validation Steps**:

1. **Signature Validation**: Verify UserOperation signature against sender account
2. **Nonce Validation**: Check nonce ordering (can be relaxed for bundler)
3. **Gas Limits**: Ensure reasonable gas limits
4. **Simulation**: Call `EntryPoint.simulateValidation(userOp)` via `eth_call`
5. **Paymaster Validation**: If paymaster present, verify deposit and signature
6. **Account Deployment**: If `initCode` present, verify factory and deployment

**Simulation Method**:

- Use existing `dryRunTx()` infrastructure
- Create a synthetic transaction calling `EntryPoint.simulateValidation(userOp)`
- Parse simulation results to extract validation errors and gas estimates

### 6. Bundling Logic - Leveraging EVM.batchRun()

**New File**: `services/requester/bundler.go`

**KEY INSIGHT**: Instead of creating a single large `EntryPoint.handleOps()` transaction, we leverage the gateway's existing `EVM.batchRun()` infrastructure to batch multiple `EntryPoint.handleOps()` transactions together in a single Cadence transaction.

```go
type Bundler struct {
    userOpPool    UserOperationPool
    validator      *UserOpValidator
    txPool         TxPool  // Reuse existing transaction pool
    config         config.Config
    logger         zerolog.Logger
}

// CreateBundledTransactions creates multiple EntryPoint.handleOps() transactions
// that will be batched together via EVM.batchRun() in a single Cadence transaction
func (b *Bundler) CreateBundledTransactions(ctx context.Context) ([]*types.Transaction, error) {
    // 1. Select UserOperations from pool
    // 2. Group into batches (respecting MaxOpsPerBundle)
    // 3. Create one EntryPoint.handleOps() transaction per batch
    // 4. Return array of transactions (will be batched by existing infrastructure)
}
```

**Optimized Bundling Strategy**:

1. **Selection Algorithm** (same as before):

   - Group UserOps by sender address
   - Sort by nonce within each sender group
   - Select profitable operations
   - Respect `MaxOpsPerBundle` limit per EntryPoint call

2. **Transaction Construction - THE KEY DIFFERENCE**:

   - **Create MULTIPLE `EntryPoint.handleOps()` transactions** instead of one
   - Each transaction handles a batch of UserOps (e.g., 10 UserOps per handleOps call)
   - Each transaction is a normal EVM transaction targeting EntryPoint
   - Example: If we have 25 UserOps with MaxOpsPerBundle=10:
     - Transaction 1: `EntryPoint.handleOps([userOp1...userOp10], beneficiary)`
     - Transaction 2: `EntryPoint.handleOps([userOp11...userOp20], beneficiary)`
     - Transaction 3: `EntryPoint.handleOps([userOp21...userOp25], beneficiary)`

3. **Leverage Existing Batching**:
   - These 3 transactions are added to the existing transaction pool
   - The `BatchTxPool` or `SingleTxPool` will collect them
   - They get wrapped in a **single Cadence transaction** using `EVM.batchRun()`
   - All 3 EntryPoint calls execute atomically in one Cadence transaction

**Benefits of This Approach**:

- ✅ **Reuses existing batching infrastructure** - No new Cadence wrapping needed
- ✅ **More efficient** - Multiple EntryPoint calls in one Cadence transaction
- ✅ **Better gas efficiency** - Single Cadence transaction overhead for multiple EntryPoint calls
- ✅ **Atomic execution** - All UserOps in the batch succeed or fail together
- ✅ **Simpler implementation** - Just create multiple EVM transactions, existing code handles the rest

### 7. Paymaster Support

**Paymaster Validation** (`services/requester/paymaster.go`):

```go
type PaymasterValidator struct {
    client *CrossSporkClient
}

func (p *PaymasterValidator) ValidatePaymaster(
    ctx context.Context,
    userOp *models.UserOperation,
    entryPoint common.Address,
) error {
    // 1. Extract paymaster address from paymasterAndData
    // 2. Check paymaster deposit in EntryPoint
    // 3. Validate paymaster signature (if present)
    // 4. Simulate paymaster validation
}
```

**Paymaster Features**:

- **Deposit Checking**: Query EntryPoint for paymaster deposit balance
- **Signature Validation**: Verify paymaster signature in `paymasterAndData`
- **Policy Enforcement**: Check paymaster-specific rules (whitelist, rate limits, etc.)
- **Gas Sponsorship**: Ensure paymaster can cover gas costs

**Note**: Paymaster logic is mostly on-chain in the EntryPoint contract. The bundler's role is to:

- Verify paymaster has sufficient deposit
- Validate paymaster signatures
- Ensure paymaster will accept the UserOperation

### 8. Integration with Existing Transaction Pool - THE CORE EFFICIENCY GAIN

**The Gateway's Unique Advantage**: Unlike traditional EVM networks where bundlers create a single `EntryPoint.handleOps()` transaction, the Flow EVM Gateway can batch **multiple** `EntryPoint.handleOps()` transactions together using `EVM.batchRun()`.

**Integration Strategy**:

1. **Bundler Creates Multiple Transactions**:

   ```go
   // Bundler collects UserOps and creates multiple EntryPoint.handleOps() transactions
   handleOpsTxs := []*types.Transaction{
       createHandleOpsTx([userOp1, userOp2, ...userOp10]),  // Batch 1
       createHandleOpsTx([userOp11, userOp12, ...userOp20]), // Batch 2
       createHandleOpsTx([userOp21, userOp22, ...userOp25]), // Batch 3
   }
   ```

2. **Add to Existing Pool**:

   - Each `handleOps` transaction is added to the existing `TxPool` (SingleTxPool or BatchTxPool)
   - They are treated as normal EVM transactions
   - The pool groups them together (especially if using BatchTxPool)

3. **Automatic Cadence Batching**:
   - The existing `run.cdc` script receives an array of transactions
   - It calls `EVM.batchRun([tx1, tx2, tx3, ...])`
   - **All EntryPoint.handleOps() calls execute in a single Cadence transaction**
   - This is more efficient than traditional networks where each handleOps is a separate transaction

**Why This is More Efficient**:

- **Traditional EVM Network**:
  - 1 UserOp → 1 EntryPoint.handleOps() → 1 on-chain transaction
  - 25 UserOps = 25 separate on-chain transactions (or 1 large handleOps with all 25)
- **Flow EVM Gateway (This Plan)**:
  - 25 UserOps → 3 EntryPoint.handleOps() transactions → **1 Cadence transaction** via `EVM.batchRun()`
  - Single Cadence transaction overhead for multiple EntryPoint calls
  - Better gas efficiency and atomicity

**Implementation**:

- No new transaction pool needed
- Bundler just creates multiple EVM transactions and adds them to existing pool
- Existing batching logic handles the rest automatically

### 9. Cadence Wrapping - Leveraging Existing Infrastructure

**No Changes Needed** - The existing `run.cdc` script already does exactly what we need:

```cadence
// Current flow (unchanged):
transaction(hexEncodedTxs: [String], coinbase: String) {
    execute {
        let txs: [[UInt8]] = []
        for tx in hexEncodedTxs {
            txs.append(tx.decodeHex())
        }

        // If multiple transactions, use EVM.batchRun
        let txResults = EVM.batchRun(
            txs: txs,  // This array can include multiple EntryPoint.handleOps() transactions
            coinbase: EVM.addressFromString(coinbase)
        )
        // ... error handling ...
    }
}
```

**How It Works for UserOps**:

1. Bundler creates multiple `EntryPoint.handleOps()` transactions
2. They get added to the transaction pool as hex-encoded strings
3. The pool batches them together (if using BatchTxPool)
4. `run.cdc` receives an array like: `[handleOpsTx1, handleOpsTx2, handleOpsTx3]`
5. `EVM.batchRun()` executes all of them in a single Cadence transaction
6. **Result**: Multiple EntryPoint calls, single Cadence transaction, maximum efficiency

**This is the key advantage**: Traditional EVM networks can't batch multiple `handleOps` calls like this. The gateway's architecture makes UserOp bundling more efficient.

### 10. Event Indexing & Receipts

**New File**: `services/ingestion/userop_events.go`

The gateway needs to index UserOperation events from EntryPoint:

```go
// Parse EntryPoint events:
// - UserOperationEvent(userOpHash, sender, paymaster, ...)
// - UserOperationRevertReason(userOpHash, ...)

func (e *Engine) indexUserOperationEvents(block *models.Block) {
    // Extract UserOperation events from EntryPoint logs
    // Map UserOperation hash to transaction hash
    // Store UserOperation receipts
}
```

**Storage** (`storage/pebble/userops.go`):

- Store UserOperation receipts
- Map `userOpHash` → `txHash` (the handleOps transaction)
- Store execution status and gas used

### 11. API Implementation Details

#### `eth_sendUserOperation` Implementation

**Location**: `api/api.go`

```go
func (b *BlockChainAPI) SendUserOperation(
    ctx context.Context,
    userOp UserOperationArgs,
    entryPoint *common.Address,
) (common.Hash, error) {
    // 1. Rate limiting
    // 2. Parse UserOperation
    // 3. Use default EntryPoint if not provided
    // 4. Validate UserOperation
    // 5. Add to UserOperation pool
    // 6. Trigger bundling (async or immediate)
    // 7. Return userOpHash
}
```

#### `eth_estimateUserOperationGas` Implementation

```go
func (b *BlockChainAPI) EstimateUserOperationGas(
    ctx context.Context,
    userOp UserOperationArgs,
    entryPoint *common.Address,
) (*UserOpGasEstimate, error) {
    // 1. Call EntryPoint.simulateValidation via eth_call
    // 2. Parse simulation results
    // 3. Estimate gas for validation + execution
    // 4. Return gas estimates
}
```

### 12. Configuration & Deployment

**Required Configuration**:

```go
// config/config.go additions
EntryPointAddress: common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"), // v0.6
BundlerEnabled: true,
MaxOpsPerBundle: 10,
UserOpTTL: 5 * time.Minute,
BundlerBeneficiary: common.Address{}, // COA address or separate
```

**EntryPoint Deployment**:

- EntryPoint contract must be deployed on Flow EVM
- Address should be standardized (CREATE2 or documented)
- Version: ERC-4337 v0.6 (current standard)

### 12.1. Wallet Discovery & Configuration

**How Wallets Discover Bundlers**:

ERC-4337 wallets (Privy, Coinbase Smart Wallet, Dynamic, etc.) do **not** auto-discover bundlers. They use hardcoded configuration mappings:

```
chainId → {
  entryPointAddress: "0x...",
  bundlerRpcUrl: "https://...",
  chainId: 123
}
```

This configuration is:

- Embedded in wallet SDKs
- Updated when new chains are onboarded
- Provided by the chain/ecosystem team to wallet providers

**What the Gateway Needs to Publish**:

For wallets to support the Flow EVM Gateway as a bundler, the following information must be provided:

1. **Bundler RPC Endpoint**: The gateway's JSON-RPC endpoint

   - Format: `http://host:port` or `https://host:port`
   - Default port: `8545` (same as standard Ethereum RPC)
   - Example: `https://evm-gateway.flow.com` or `http://localhost:8545`

2. **EntryPoint Address**: The deployed EntryPoint contract address

   - Must be on Flow EVM
   - Should use CREATE2 for deterministic address (like mainnet: `0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789`)

3. **Chain ID**: The EVM chain ID for Flow EVM

   - From `config.EVMNetworkID`
   - Used to identify the network

4. **Network Name**: Human-readable name
   - Example: "Flow Mainnet EVM", "Flow Testnet EVM"

**Sample Privy Configuration**:

```typescript
// privy-config.ts
import { PrivyClientConfig } from "@privy-io/react-auth";

export const privyConfig: PrivyClientConfig = {
  appId: "your-privy-app-id",

  // Configure supported chains
  supportedChains: [
    {
      id: 747, // Flow EVM Chain ID (example - use actual Flow EVM chain ID)
      name: "Flow Mainnet EVM",
      network: "flow-mainnet-evm",
      nativeCurrency: {
        name: "Flow",
        symbol: "FLOW",
        decimals: 18,
      },
      rpcUrls: {
        default: {
          http: ["https://evm-gateway.flow.com"], // Gateway RPC endpoint
        },
        public: {
          http: ["https://evm-gateway.flow.com"],
        },
      },
      blockExplorers: {
        default: {
          name: "Flow EVM Explorer",
          url: "https://evm-explorer.flow.com",
        },
      },
    },
  ],

  // ERC-4337 Configuration
  embeddedWallets: {
    createOnLogin: "users-without-wallets",
    requireUserPasswordOnCreate: false,
    // Privy will use the RPC endpoint above for bundler operations
    // EntryPoint address is configured in Privy's dashboard or via API
  },

  // Additional Privy config...
};
```

**Privy Smart Wallet Provider Configuration**:

```typescript
// When using Privy's smart wallet SDK directly
import { createSmartWalletClient } from "@privy-io/react-auth";

const smartWallet = createSmartWalletClient({
  // Chain configuration
  chain: {
    id: 747, // Flow EVM Chain ID
    name: "Flow Mainnet EVM",
    network: "flow-mainnet-evm",
    nativeCurrency: {
      name: "Flow",
      symbol: "FLOW",
      decimals: 18,
    },
    rpcUrls: {
      default: {
        http: ["https://evm-gateway.flow.com"],
      },
    },
  },

  // ERC-4337 Bundler Configuration
  bundler: {
    // Privy can use a custom bundler endpoint
    rpcUrl: "https://evm-gateway.flow.com", // Gateway's RPC endpoint
    // EntryPoint address is typically configured per-chain
    // Privy may require this via their dashboard or API
  },

  // EntryPoint address (if required by Privy SDK)
  entryPoint: "0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789", // Or Flow EVM EntryPoint address
});
```

**Alternative: Direct Bundler RPC Configuration**:

Some wallets allow direct bundler endpoint configuration:

```typescript
// Example with ZeroDev or similar SDKs
import { createSmartAccountClient } from "@zerodev/sdk";

const smartAccount = await createSmartAccountClient({
  chain: flowEvmChain,
  // Direct bundler configuration
  bundlerRpc: "https://evm-gateway.flow.com", // Gateway RPC
  entryPoint: "0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789",
  // ... other config
});
```

**What the Gateway Must Expose**:

The gateway's RPC endpoint must support the standard ERC-4337 bundler methods:

1. `eth_sendUserOperation` - Accept UserOps
2. `eth_estimateUserOperationGas` - Estimate gas for UserOps
3. `eth_getUserOperationByHash` - Query UserOp status
4. `eth_getUserOperationReceipt` - Get UserOp receipt

Plus standard Ethereum RPC methods:

- `eth_chainId` - Return Flow EVM chain ID
- `eth_getBlockByNumber` - For state queries
- `eth_call` - For simulation
- `eth_getTransactionReceipt` - For transaction tracking

**Onboarding Process for Wallets**:

1. **Deploy EntryPoint**: Deploy EntryPoint contract on Flow EVM
2. **Document Configuration**: Publish bundler endpoint and EntryPoint address
3. **Contact Wallet Providers**: Reach out to Privy, Coinbase, Dynamic, etc.
4. **Provide Testnet Access**: Give wallet providers access to testnet bundler
5. **Integration Testing**: Work with wallet teams to test integration
6. **Mainnet Launch**: Coordinate mainnet launch with wallet support

**Example Documentation Format**:

```markdown
# Flow EVM ERC-4337 Configuration

## Network Information

- **Chain ID**: 747 (example)
- **Network Name**: Flow Mainnet EVM
- **Native Currency**: FLOW (18 decimals)

## EntryPoint

- **Address**: `0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789` (v0.6)
- **Version**: ERC-4337 v0.6

## Bundler

- **RPC Endpoint**: `https://evm-gateway.flow.com`
- **WebSocket**: `wss://evm-gateway.flow.com` (if supported)
- **Methods**: `eth_sendUserOperation`, `eth_estimateUserOperationGas`, etc.

## Testnet

- **Chain ID**: 545 (example)
- **Bundler**: `https://evm-gateway-testnet.flow.com`
- **EntryPoint**: Same address (if using CREATE2)
```

**Gateway RPC Endpoint Configuration**:

The gateway already exposes RPC on `RPCHost:RPCPort` (default: `:8545`). For production:

```bash
# Gateway configuration
--rpc-host=0.0.0.0  # Listen on all interfaces
--rpc-port=8545     # Standard Ethereum RPC port
--ws-enabled=true   # Enable WebSocket support
```

The bundler methods will be automatically available on the same endpoint once implemented.

### 13. Error Handling

**New Error Types** (`models/errors/errors.go`):

```go
ErrInvalidUserOpSignature
ErrUserOpNonceTooLow
ErrPaymasterDepositInsufficient
ErrPaymasterValidationFailed
ErrUserOpSimulationFailed
ErrUserOpExpired
ErrDuplicateUserOp
```

### 14. Metrics & Observability

**New Metrics** (`metrics/collector.go`):

```go
UserOperationsReceived()
UserOperationsBundled()
UserOperationsDropped()
UserOperationsExpired()
BundlesCreated()
BundleGasUsed()
PaymasterOperations()
```

## Implementation Phases

### Phase 1: Foundation

1. Add UserOperation data structures
2. Implement basic UserOperation pool (in-memory)
3. Add EntryPoint address to config
4. Implement UserOperation hash calculation

### Phase 2: Validation

1. Implement UserOperation signature validation
2. Implement `simulateValidation` call
3. Add paymaster validation
4. Implement gas estimation

### Phase 3: Bundling (Leveraging EVM.batchRun)

1. Implement bundling logic that creates **multiple** `handleOps` transactions
2. Create `handleOps` transaction construction (one per batch of UserOps)
3. Add multiple transactions to existing transaction pool (they'll be batched automatically)
4. Test that multiple `handleOps` transactions get batched into one Cadence transaction
5. Verify `EVM.batchRun()` executes all EntryPoint calls atomically

### Phase 4: API

1. Implement `eth_sendUserOperation`
2. Implement `eth_estimateUserOperationGas`
3. Implement `eth_getUserOperationByHash`
4. Implement `eth_getUserOperationReceipt`

### Phase 5: Indexing

1. Parse EntryPoint events
2. Store UserOperation receipts
3. Map UserOp hashes to transaction hashes

### Phase 6: Production Readiness

1. Add comprehensive error handling
2. Add metrics and observability
3. Add rate limiting for UserOps
4. Performance testing and optimization
5. Documentation

## Key Design Decisions

### 1. Reuse Existing Infrastructure

- **Decision**: Reuse `TxPool` interface and Cadence wrapping
- **Rationale**: EntryPoint transactions are just EVM transactions. No need to duplicate wrapping logic.

### 2. Separate UserOp Pool

- **Decision**: Maintain separate alt-mempool for UserOperations
- **Rationale**: UserOps have different validation, batching, and lifecycle than regular transactions.

### 3. Bundling Strategy

- **Decision**: Time-based batching with profitability filtering
- **Rationale**: Similar to existing `BatchTxPool`, but with UserOp-specific selection logic.

### 4. Paymaster Support

- **Decision**: Full paymaster validation and support
- **Rationale**: Essential for gasless transactions, a key 4337 use case.

### 5. EntryPoint Address

- **Decision**: Configurable with sensible defaults
- **Rationale**: Allows per-network configuration and testing with different EntryPoint versions.

## Open Questions & Considerations

1. **Bundler Economics**:

   - How to price UserOps? (fee market, priority fees)
   - How to handle unprofitable UserOps?
   - Should bundler accept UserOps with zero fees?

2. **Nonce Management**:

   - How to handle nonce gaps in UserOp batching?
   - Should bundler wait for missing nonces or skip?

3. **Paymaster Integration**:

   - Support for token paymasters? (ERC-20 gas payment)
   - How to handle paymaster signature validation?

4. **EntryPoint Version**:

   - Which EntryPoint version to support? (v0.6 is current)
   - How to handle EntryPoint upgrades?

5. **Rate Limiting**:

   - Should UserOps have separate rate limits?
   - How to prevent UserOp spam?

6. **State Validation**:

   - Use `LocalIndexValidation` or `TxSealValidation` for UserOps?
   - How to handle UserOp validation failures?

7. **Batching Frequency**:
   - Time-based (like current BatchTxPool)?
   - Size-based (when N UserOps collected)?
   - Hybrid approach?

## Testing Strategy

1. **Unit Tests**:

   - UserOperation hash calculation
   - Signature validation
   - Paymaster validation
   - Bundling logic

2. **Integration Tests**:

   - End-to-end UserOp submission
   - Bundling and Cadence wrapping
   - EntryPoint execution
   - Receipt generation

3. **E2E Tests**:
   - Full flow: Wallet → Gateway → EntryPoint → Execution
   - Paymaster sponsorship
   - Account deployment
   - Error scenarios

## Dependencies

1. **EntryPoint Contract**: Must be deployed on Flow EVM
2. **Account Factories**: Standard account factories for account deployment
3. **Paymaster Contracts**: Optional, but needed for gas sponsorship
4. **EntryPoint ABI**: For encoding/decoding handleOps calls

## References

- [ERC-4337 Specification](https://eips.ethereum.org/EIPS/eip-4337)
- [EntryPoint v0.6 Contract](https://github.com/eth-infinitism/account-abstraction/blob/develop/contracts/core/EntryPoint.sol)
- [Bundler Specification](https://github.com/eth-infinitism/bundler-spec)

## Conclusion - Leveraging Gateway's Unique Architecture

The EVM Gateway is **uniquely positioned** to support ERC-4337 more efficiently than traditional EVM networks because of its Cadence batching architecture.

### Key Architectural Advantage

**Traditional EVM Networks**:

- Bundler creates: `EntryPoint.handleOps([userOp1, userOp2, ..., userOpN], beneficiary)`
- This becomes: **1 on-chain transaction**
- To batch more UserOps, you put them all in one handleOps call (limited by gas)

**Flow EVM Gateway (This Plan)**:

- Bundler creates: Multiple `EntryPoint.handleOps()` transactions
  - `EntryPoint.handleOps([userOp1...userOp10], beneficiary)`
  - `EntryPoint.handleOps([userOp11...userOp20], beneficiary)`
  - `EntryPoint.handleOps([userOp21...userOp25], beneficiary)`
- These become: **1 Cadence transaction** via `EVM.batchRun()`
- **All EntryPoint calls execute atomically in a single Cadence transaction**

### Efficiency Gains

1. **Better Batching**: Can batch across multiple EntryPoint calls, not just within one
2. **Single Cadence Overhead**: One Cadence transaction for multiple EntryPoint executions
3. **Atomic Execution**: All UserOps in the batch succeed or fail together
4. **Reuses Existing Infrastructure**: No new wrapping logic needed

### Implementation Summary

The existing architecture (transaction pools, Cadence wrapping, `EVM.batchRun()`) can be extended with:

1. **UserOperation data structures and alt-mempool** - Store UserOps separately
2. **EntryPoint validation/simulation** - Validate UserOps before batching
3. **Bundling logic** - Create multiple `EntryPoint.handleOps()` transactions
4. **New JSON-RPC methods** - Standard 4337 bundler API
5. **Integration with existing pool** - Add handleOps transactions to existing TxPool

**The key insight**: Create multiple `EntryPoint.handleOps()` EVM transactions, add them to the existing transaction pool, and let `EVM.batchRun()` handle the rest. This leverages the gateway's unique architecture for maximum efficiency.
