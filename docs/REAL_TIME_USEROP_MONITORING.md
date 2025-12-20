# Real-Time UserOp Monitoring

## Current Situation

Bundler is running but finding 0 pending UserOps. This could mean:
- ✅ UserOp was already processed (good!)
- ⚠️ UserOp expired (TTL)
- ❌ UserOp validation failed and wasn't added to pool

## Monitor Full Flow in Real-Time

### Step 1: Start Monitoring (Before Submitting)

Open a terminal and run this to watch all UserOp activity:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -E "userop|SendUserOperation|bundler|pendingUserOpCount|created handleOps|submitted bundled|removed UserOp|validation"
```

### Step 2: Submit a New UserOp

In another terminal or your frontend, submit a new UserOp.

### Step 3: Watch the Flow

You should see this sequence:

#### A. UserOp Received
```
"component":"userop-api"
"endpoint":"SendUserOperation"
"message":"received eth_sendUserOperation request"
```

#### B. Validation Started
```
"component":"userop-validator"
"message":"calling EntryPoint.simulateValidation with full UserOp details"
```

#### C. Validation Result
Either:
- ✅ Success: `"message":"EntryPoint.simulateValidation succeeded"`
- ❌ Failure: `"error":"..."` and `"message":"EntryPoint.simulateValidation reverted"`

#### D. Added to Pool (if validation succeeded)
```
"component":"userop-api"
"message":"user operation added to pool - will be included in next bundle"
```

#### E. Bundler Picks It Up (within ~1 second)
```
"component":"bundler"
"pendingUserOpCount":1
"message":"found pending UserOperations - creating bundled transactions"
```

#### F. Transaction Created
```
"component":"bundler"
"message":"created handleOps transaction"
"txHash":"0x..."
```

#### G. UserOp Removed
```
"component":"bundler"
"message":"removed UserOp from pool after bundling"
```

#### H. Transaction Submitted
```
"component":"bundler"
"message":"submitted bundled transaction to pool"
```

## Check What Happened to Previous UserOp

### Option 1: Check Recent UserOp Activity

```bash
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | grep -E "SendUserOperation|user operation added to pool|validation.*failed|validation.*succeeded" | tail -20
```

### Option 2: Check for Validation Failures

```bash
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | grep -E "validation.*failed|simulateValidation.*reverted|user operation validation failed" | tail -20
```

### Option 3: Check if UserOp Was Added Then Removed

```bash
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | grep -E "0xf39f55c63cc6b7cfc10b28509ec120f3c38a738eac394f576d53707ba4cd973a|user operation added to pool|removed UserOp from pool"
```

## If UserOp Was Already Processed

If the UserOp hash `0xf39f55c63cc6b7cfc10b28509ec120f3c38a738eac394f576d53707ba4cd973a` was processed, you should see:

1. **Transaction created**: Look for logs with that hash or the sender address `0x71ee4bc503BeDC396001C4c3206e88B965c6f860`

2. **Account created**: Check if the account now has code:
   ```bash
   curl -X POST http://3.150.43.95:8545 \
     -H 'Content-Type: application/json' \
     --data '{
       "jsonrpc": "2.0",
       "id": 1,
       "method": "eth_getCode",
       "params": ["0x71ee4bc503BeDC396001C4c3206e88B965c6f860", "latest"]
     }'
   ```

3. **Events**: Check for EntryPoint events:
   ```bash
   curl -X POST http://3.150.43.95:8545 \
     -H 'Content-Type: application/json' \
     --data '{
       "jsonrpc": "2.0",
       "id": 1,
       "method": "eth_getLogs",
       "params": [{
         "fromBlock": "latest",
         "toBlock": "latest",
         "address": "0xcf1e8398747a05a997e8c964e957e47209bdff08",
         "topics": ["0x..." /* UserOperationEvent topic */]
       }]
     }'
   ```

## Quick Test: Submit New UserOp

The best way to verify everything is working is to submit a fresh UserOp and watch the logs in real-time. The bundler should pick it up within 1 second and process it.

## Expected Timeline

- **T+0ms**: UserOp submitted
- **T+0-100ms**: Validation
- **T+100ms**: Added to pool (if validation succeeds)
- **T+100-900ms**: Next bundler tick
- **T+900-1100ms**: Transaction created
- **T+1100ms**: UserOp removed from pool
- **T+1100ms**: Transaction submitted
- **T+1-5 seconds**: Included in block

