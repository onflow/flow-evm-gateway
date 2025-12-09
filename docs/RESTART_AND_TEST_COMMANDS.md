# Quick Restart and Test Commands

## On EC2 Instance (SSH in first)

### 1. Restart Service

```bash
# Reload systemd (if config changed)
sudo systemctl daemon-reload

# Restart the service
sudo systemctl restart flow-evm-gateway

# Check status
sudo systemctl status flow-evm-gateway --no-pager
```

### 2. Verify Service is Running

```bash
# Check Docker container
docker ps | grep flow-evm-gateway

# Check recent logs
sudo journalctl -u flow-evm-gateway -n 50 --no-pager
```

### 3. Test Basic RPC

```bash
# Test eth_blockNumber
docker run --rm --network container:flow-evm-gateway curlimages/curl \
  -s -X POST http://127.0.0.1:8545 \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

### 4. Monitor Logs for UserOp Activity

```bash
# Watch logs in real-time (filtered for UserOp activity)
sudo journalctl -u flow-evm-gateway -f | grep -iE "userop|sendUserOperation|getUserOpHash|userOpHash_from_contract|AA24|AA13|signature|entrypoint"
```

### 5. Check Last Submitted UserOp

```bash
# Check for the last UserOp submission and hash calculation
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -iE "received eth_sendUserOperation|userOpHash_from_contract|user operation added to pool|AA24|AA13" | \
  tail -20
```

### 6. Verify getUserOpHash is Being Called

```bash
# Check that EntryPoint.getUserOpHash() is being called (should see "userOpHash_from_contract")
sudo journalctl -u flow-evm-gateway --since "10 minutes ago" | \
  grep -i "userOpHash_from_contract" | \
  tail -10
```

### 7. Check for Hash Mismatch Warnings (Should be NONE)

```bash
# Should return nothing if fix is working
sudo journalctl -u flow-evm-gateway --since "10 minutes ago" | \
  grep -iE "hash.*mismatch|failed to get UserOp hash|falling back to manual"
```

### 8. Monitor Bundler Activity

```bash
# Check bundler is processing UserOps
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -iE "bundler tick|found pending|created handleOps|submitted.*transaction" | \
  tail -20
```

### 9. Check Transaction Execution and UserOp Indexing

```bash
# Check if UserOps are being indexed successfully
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -iE "indexed.*UserOperation|AA24|AA23|AA21|AA13|success.*true|success.*false" | \
  tail -30
```

## Expected Behavior After Fix

### ✅ What You Should See:

1. **getUserOpHash calls**: Logs showing `"userOpHash_from_contract"` with hash values
2. **No manual hash fallbacks**: No errors about "failed to get UserOp hash" or "falling back to manual"
3. **No hash mismatch warnings**: No warnings about hash mismatches
4. **Successful UserOp processing**: UserOps should be added to pool and bundled successfully
5. **No AA24 errors**: If signature is correct, should not see AA24 signature errors

### ❌ What You Should NOT See:

1. ❌ `"failed to get UserOp hash from EntryPoint.getUserOpHash()"`
2. ❌ `"falling back to manual hash calculation"`
3. ❌ `"hash mismatch"` warnings
4. ❌ `"requester is nil"` errors

## Quick Test: Submit a UserOp and Verify

After restarting, when a UserOp is submitted:

1. **Check hash calculation**:
   ```bash
   sudo journalctl -u flow-evm-gateway --since "1 minute ago" | \
     grep -i "userOpHash_from_contract"
   ```
   Should show the hash returned by EntryPoint.getUserOpHash()

2. **Check UserOp was added to pool**:
   ```bash
   sudo journalctl -u flow-evm-gateway --since "1 minute ago" | \
     grep -i "user operation added to pool"
   ```

3. **Check bundler picked it up**:
   ```bash
   sudo journalctl -u flow-evm-gateway --since "1 minute ago" | \
     grep -iE "bundler tick|found pending|created handleOps"
   ```

## Troubleshooting

### If service won't start:

```bash
# Check detailed logs
sudo journalctl -u flow-evm-gateway -n 100 --no-pager

# Check Docker logs directly
docker logs flow-evm-gateway --tail 50
```

### If getUserOpHash is not being called:

1. Verify requester is initialized:
   ```bash
   sudo journalctl -u flow-evm-gateway | grep -i "requester.*nil"
   ```
   Should return nothing

2. Check EntryPoint address is configured:
   ```bash
   sudo cat /etc/flow/runtime-conf.env | grep ENTRY_POINT
   ```

### If still seeing AA24 errors:

1. Check the hash matches frontend:
   ```bash
   sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
     grep -A 5 "userOpHash_from_contract"
   ```

2. Compare with frontend hash - they should match exactly

3. Check signature format:
   ```bash
   sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
     grep -iE "signature.*v|signature.*length|signature.*hex"
   ```

