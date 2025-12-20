# How to Verify UserOp is Being Included

## After UserOp is Accepted

Once you see `"result": "0x..."` from `eth_sendUserOperation`, the UserOp is in the pool. Here's how to verify it's being processed and included.

## Step 1: Check Bundler Activity

```bash
# Watch for bundler processing this specific UserOp
sudo journalctl -u flow-evm-gateway -f | grep -E "0xf39f55c63cc6b7cfc10b28509ec120f3c38a738eac394f576d53707ba4cd973a|pendingUserOpCount|created handleOps|submitted bundled"
```

**Expected logs** (within ~1 second):
- `"bundler tick - checking for pending UserOperations"`
- `"found pending UserOperations - creating bundled transactions"`
- `"created handleOps transaction"` with `txHash`
- `"removed UserOp from pool after bundling"`
- `"submitted bundled transaction to pool"`

## Step 2: Check Transaction Pool

```bash
# Check if transaction was added to pool
sudo journalctl -u flow-evm-gateway -f | grep -E "submitted bundled transaction|txHash.*0x"
```

**Expected**: Should see transaction hash being submitted.

## Step 3: Check Block Inclusion

```bash
# Watch for block execution events
sudo journalctl -u flow-evm-gateway -f | grep -E "new evm block executed|handleOps|0x71ee4bc503BeDC396001C4c3206e88B965c6f860"
```

**Expected**: Should see the account creation transaction in a block.

## Step 4: Verify Account Creation

After a few seconds, check if the account was created:

```bash
# Check account code (should be non-empty after creation)
curl -X POST http://3.150.43.95:8545 \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "eth_getCode",
    "params": ["0x71ee4bc503BeDC396001C4c3206e88B965c6f860", "latest"]
  }'
```

**Expected**: Should return non-empty code (account was created).

## Step 5: Check EntryPoint Events

```bash
# Check for account creation events
curl -X POST http://3.150.43.95:8545 \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "eth_getLogs",
    "params": [{
      "fromBlock": "latest",
      "toBlock": "latest",
      "address": "0xcf1e8398747a05a997e8c964e957e47209bdff08"
    }]
  }'
```

**Expected**: Should see `UserOperationEvent` logs for your UserOp.

## Troubleshooting

### If Bundler Doesn't Process

**Check bundler is enabled:**
```bash
sudo cat /etc/flow/runtime-conf.env | grep BUNDLER_ENABLED
```

**Check bundler errors:**
```bash
sudo journalctl -u flow-evm-gateway --since "1 minute ago" | grep -iE "bundler.*error|failed.*create.*handleOps"
```

### If Transaction Creation Fails

**Check encoding errors:**
```bash
sudo journalctl -u flow-evm-gateway --since "1 minute ago" | grep -iE "failed to encode|encoding"
```

**If you see encoding errors**: The fix may not be deployed yet.

### If Transaction Submission Fails

**Check transaction pool errors:**
```bash
sudo journalctl -u flow-evm-gateway --since "1 minute ago" | grep -iE "failed.*add.*handleOps|txPool.*error"
```

## Success Indicators

✅ **UserOp accepted**: Got hash back from `eth_sendUserOperation`  
✅ **Bundler processing**: See "created handleOps transaction" logs  
✅ **Transaction submitted**: See "submitted bundled transaction" logs  
✅ **Account created**: `eth_getCode` returns non-empty code  
✅ **Events emitted**: See `UserOperationEvent` logs  

## Timeline

- **T+0ms**: UserOp submitted, accepted, hash returned
- **T+0-800ms**: Next bundler tick
- **T+800-1000ms**: Transaction created and submitted
- **T+1-5 seconds**: Transaction included in block
- **T+5-10 seconds**: Account created, events emitted

## Your Current Status

Based on your logs:
- ✅ UserOp accepted: `0xf39f55c63cc6b7cfc10b28509ec120f3c38a738eac394f576d53707ba4cd973a`
- ✅ Hash matches: Client and gateway agree
- ⏳ **Next**: Wait for bundler to process (check logs above)

