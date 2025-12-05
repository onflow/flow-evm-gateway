# UserOperation Handling Implementation

## Overview

The EVM Gateway implements ERC-4337 UserOperation handling, allowing users to submit UserOperations that are validated, pooled, and automatically bundled into EntryPoint transactions. This document describes the current implementation and how it works.

## Architecture

The UserOperation handling system consists of four main components:

1. **UserOpAPI** - RPC endpoint handler for `eth_sendUserOperation`
2. **UserOpValidator** - Validates UserOperations before acceptance
3. **UserOpPool** - In-memory pool for pending UserOperations
4. **Bundler** - Periodically bundles UserOperations into EntryPoint transactions

## Component Details

### 1. UserOpAPI (`api/userop_api.go`)

The `UserOpAPI` handles incoming RPC requests for UserOperation submission.

#### Endpoints

- **`eth_sendUserOperation`**: Accepts a UserOperation, validates it, and adds it to the pool
- **`eth_getUserOperationByHash`**: Retrieves a UserOperation by its hash
- **`eth_getUserOperationReceipt`**: Gets the receipt for a UserOperation
- **`eth_estimateUserOperationGas`**: Estimates gas for a UserOperation

#### SendUserOperation Flow

1. **Rate Limiting**: Checks if request exceeds rate limits
2. **EntryPoint Selection**: Uses configured EntryPoint or provided one
3. **Conversion**: Converts `UserOperationArgs` to `UserOperation` model
4. **Validation**: Calls `UserOpValidator.Validate()` (signature, simulation)
5. **Pool Addition**: Adds validated UserOperation to pool via `userOpPool.Add()`
6. **Async Trigger**: Triggers bundling in background (non-blocking)

**Key Behavior**: UserOperations are only added to the pool if validation succeeds. Invalid UserOperations are rejected immediately.

### 2. UserOpValidator (`services/requester/userop_validator.go`)

The `UserOpValidator` performs comprehensive validation before accepting a UserOperation.

#### Validation Steps

1. **Signature Recovery**: Recovers signer from UserOperation hash
2. **Owner Verification**: For account creation (initCode), verifies signer matches owner from initCode
3. **Simulation**: Calls `EntryPointSimulations.simulateValidation()` via `eth_call`
4. **AA13 Handling**: For account creation UserOperations, AA13 errors are expected and allowed

#### EntryPointSimulations Contract

- Uses a separately deployed `EntryPointSimulations` contract for validation
- Required for EntryPoint v0.7+ which doesn't have `simulateValidation` directly
- Address configured via `--entry-point-simulations-address` flag
- Falls back to EntryPoint address if not configured (for v0.6 compatibility)

#### Account Creation Handling

- **AA13 Error**: Expected for account creation UserOperations during simulation
- **Reason**: `simulateValidation` runs in STATICCALL context, which prevents CREATE2
- **Behavior**: Gateway treats AA13 as expected and proceeds to enqueue the UserOperation
- **Note**: The actual `handleOps` transaction will succeed because it runs in a non-static context

### 3. UserOpPool (`services/requester/userop_pool.go`)

The `InMemoryUserOpPool` manages pending UserOperations in memory.

#### Features

- **In-Memory Storage**: All UserOperations stored in memory (not persisted)
- **TTL Support**: UserOperations expire after configured TTL (default: 5 minutes)
- **Sender Grouping**: Tracks UserOperations by sender address
- **Hash Indexing**: Fast lookup by UserOperation hash
- **Expiration**: Background goroutine removes expired UserOperations

#### Methods

- **`Add(ctx, userOp, entryPoint)`**: Adds UserOperation to pool, returns hash
- **`GetPending()`**: Returns all non-expired UserOperations
- **`GetByHash(hash)`**: Retrieves UserOperation by hash
- **`GetBySender(sender)`**: Gets all UserOperations for a sender
- **`Remove(userOp)`**: Removes UserOperation from pool
- **`RemoveByHash(hash)`**: Removes UserOperation by hash

#### TTL Configuration

- Configured via `--user-op-ttl` flag (default: 5 minutes)
- Expired UserOperations are automatically removed
- Prevents pool from growing unbounded

### 4. Bundler (`services/requester/bundler.go`)

The `Bundler` periodically processes pending UserOperations and creates EntryPoint transactions.

#### Periodic Execution

- **Interval**: Configurable via `--bundler-interval` (default: 800ms)
- **Execution**: Runs continuously in background goroutine
- **Trigger**: Timer-based, not event-driven
- **Location**: Started in `bootstrap/bootstrap.go::StartAPIServer()`

#### CreateBundledTransactions Flow

1. **Get Pending**: Retrieves all pending UserOperations from pool
2. **Group by Sender**: Groups UserOperations by sender address
3. **Sort by Nonce**: Sorts UserOperations within each sender group by nonce
4. **Batch**: Splits into batches respecting `MaxOpsPerBundle` limit
5. **Create Transactions**: For each batch, creates an `EntryPoint.handleOps()` transaction
6. **Return**: Returns transactions with associated UserOperations

#### Transaction Creation

The `createHandleOpsTransaction()` method:

1. **Encode Calldata**: Encodes `handleOps(userOps, beneficiary)` call
2. **Gas Estimation**: Estimates gas for the transaction
3. **Nonce Calculation**: Gets next nonce from network, accounting for pending transactions
4. **Transaction Building**: Creates signed transaction with:
   - From: Coinbase address (bundler signer)
   - To: EntryPoint address
   - Gas: Estimated gas limit
   - GasPrice: Configured gas price
   - Nonce: Calculated nonce
   - Data: Encoded handleOps calldata
5. **Signing**: Signs transaction with bundler's private key

#### SubmitBundledTransactions Flow

1. **Create Transactions**: Calls `CreateBundledTransactions()`
2. **Submit to Pool**: For each transaction, calls `txPool.Add()`
3. **Remove UserOps**: Only removes UserOperations from pool **after** successful submission
4. **Error Handling**: If submission fails, UserOperations remain in pool for retry

**Key Behavior**: UserOperations are only removed from pool after transaction is successfully submitted to the transaction pool. This prevents loss if submission fails.

#### Batching Logic

- **Sender Grouping**: UserOperations from same sender are grouped together
- **Nonce Ordering**: Within each sender group, sorted by nonce (ascending)
- **Batch Size**: Respects `MaxOpsPerBundle` limit (default: 10)
- **Multiple Batches**: If sender has more than `MaxOpsPerBundle` UserOperations, creates multiple batches

#### Configuration

- **`BundlerEnabled`**: Must be `true` for bundler to run
- **`BundlerInterval`**: Time between bundler ticks (default: 800ms)
- **`MaxOpsPerBundle`**: Maximum UserOperations per transaction (default: 10)
- **`UserOpTTL`**: Time to live for UserOperations in pool (default: 5 minutes)
- **`EntryPointAddress`**: Address of EntryPoint contract (required)
- **`EntryPointSimulationsAddress`**: Address of EntryPointSimulations contract (required for v0.7+)
- **`BundlerBeneficiary`**: Address to receive fees from EntryPoint (optional)

## Data Flow

### Complete UserOperation Lifecycle

```
1. User submits UserOperation via eth_sendUserOperation
   ↓
2. UserOpAPI receives request
   ↓
3. UserOpValidator validates:
   - Signature recovery
   - Owner verification (for account creation)
   - EntryPointSimulations.simulateValidation()
   ↓
4. UserOperation added to UserOpPool
   ↓
5. [WAIT] Bundler timer fires (every 800ms)
   ↓
6. Bundler.CreateBundledTransactions():
   - Gets pending UserOperations
   - Groups by sender
   - Sorts by nonce
   - Batches (MaxOpsPerBundle)
   - Creates handleOps transactions
   ↓
7. Bundler.SubmitBundledTransactions():
   - Submits transactions to TxPool
   - Removes UserOperations from pool (after success)
   ↓
8. TxPool processes transactions
   ↓
9. Transactions included in block
   ↓
10. UserOperations executed via EntryPoint.handleOps()
```

## Key Design Decisions

### 1. In-Memory Pool

- **Rationale**: Fast access, simple implementation
- **Trade-off**: UserOperations lost on gateway restart
- **Mitigation**: TTL prevents unbounded growth

### 2. Periodic Bundling

- **Rationale**: Predictable latency, efficient batching
- **Trade-off**: Up to 800ms delay before bundling
- **Mitigation**: Configurable interval, async trigger on submission

### 3. Post-Submission Removal

- **Rationale**: Prevents loss if transaction submission fails
- **Trade-off**: UserOperations may be included in multiple transactions if pool not updated
- **Mitigation**: UserOperations removed immediately after successful submission

### 4. EntryPointSimulations Contract

- **Rationale**: Required for EntryPoint v0.7+ which lacks `simulateValidation`
- **Trade-off**: Additional contract deployment
- **Mitigation**: Falls back to EntryPoint address for v0.6 compatibility

### 5. AA13 Handling for Account Creation

- **Rationale**: `simulateValidation` runs in STATICCALL context, preventing CREATE2
- **Behavior**: AA13 errors are expected and allowed for account creation UserOperations
- **Note**: Actual `handleOps` transaction succeeds because it runs in non-static context

## Error Handling

### Validation Failures

- **Invalid Signature**: UserOperation rejected immediately
- **Simulation Failure**: UserOperation rejected (except AA13 for account creation)
- **Rate Limit Exceeded**: Request rejected with rate limit error

### Bundling Failures

- **Transaction Creation Fails**: UserOperations remain in pool, retried on next tick
- **Transaction Submission Fails**: UserOperations remain in pool, retried on next tick
- **Encoding Errors**: UserOperations remain in pool, retried on next tick

### Pool Management

- **TTL Expiration**: UserOperations automatically removed after TTL
- **Gateway Restart**: All UserOperations lost (in-memory pool)

## Configuration Requirements

### Required Flags

- `--bundler-enabled`: Must be `true`
- `--entry-point-address`: EntryPoint contract address
- `--entry-point-simulations-address`: EntryPointSimulations contract address (v0.7+)
- `--coinbase`: Bundler signer address
- `--wallet-key`: Private key for bundler signer (if using wallet API)

### Optional Flags

- `--bundler-interval`: Bundler tick interval (default: 800ms)
- `--max-ops-per-bundle`: Maximum UserOperations per transaction (default: 10)
- `--user-op-ttl`: Time to live for UserOperations (default: 5 minutes)
- `--bundler-beneficiary`: Address to receive EntryPoint fees

## Testing

### Unit Tests

- `bundler_test.go`: Tests bundler batching, grouping, and transaction creation
- `userop_pool_test.go`: Tests pool operations, TTL, and expiration
- `userop_validator_test.go`: Tests validation logic and error handling

### Integration Tests

- End-to-end UserOperation submission and execution
- Account creation flow
- Multiple UserOperations from same sender
- TTL expiration behavior

## Performance Characteristics

### Latency

- **Submission to Pool**: < 100ms (validation + pool addition)
- **Pool to Transaction**: 0-800ms (depends on bundler tick timing)
- **Transaction to Block**: Depends on block time (typically seconds)

### Throughput

- **Validation**: Limited by RPC calls to EntryPointSimulations
- **Bundling**: Limited by `MaxOpsPerBundle` and bundler interval
- **Transaction Submission**: Limited by transaction pool capacity

### Resource Usage

- **Memory**: Proportional to number of pending UserOperations
- **CPU**: Periodic bundler ticks, validation on submission
- **Network**: RPC calls for validation and transaction submission

