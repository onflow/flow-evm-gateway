# Reset and Verify getUserOpHash Fix

## Quick Summary

This fix ensures `EntryPoint.getUserOpHash()` is called with an **empty signature** (`[]byte{}`), matching the ERC-4337 spec. The hash is what gets signed, so it cannot include the signature itself.

## Step 1: Build and Push New Image (Local Machine)

```bash
cd /Users/briandoyle/src/evm-gateway

# Set version tag
export VERSION=no-keys-logging-getuserophash-fix

# Build for Linux/AMD64 (Apple Silicon Macs)
docker buildx build --platform linux/amd64 \
  --build-arg VERSION="${VERSION}" \
  --build-arg ARCH=amd64 \
  -f Dockerfile \
  -t flow-evm-gateway:${VERSION} \
  --load .

# Authenticate with ECR
export AWS_ACCOUNT_ID=000338030955
export AWS_REGION=us-east-2

aws ecr get-login-password --region ${AWS_REGION} | \
  docker login --username AWS --password-stdin \
  ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com

# Tag and push
docker tag flow-evm-gateway:${VERSION} \
  ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}

docker push ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/flow-evm-gateway:${VERSION}
```

## Step 2: Update and Restart Service (EC2 Instance)

```bash
# SSH to EC2
ssh -i ~/Downloads/demo.pem ec2-user@ec2-3-150-43-95.us-east-2.compute.amazonaws.com

# Update version in config
sudo nano /etc/flow/runtime-conf.env
# Change: VERSION=no-keys-logging-getuserophash-fix

# Reload and restart
sudo systemctl daemon-reload
sudo systemctl restart flow-evm-gateway

# Verify service is running
sudo systemctl status flow-evm-gateway --no-pager
```

## Step 3: Verify the Fix - Check Logs

### 3.1: Check getUserOpHash Calls (Should Use Empty Signature)

```bash
# Check if getUserOpHash is being called (should see no errors about signature)
sudo journalctl -u flow-evm-gateway --since "2 minutes ago" | \
  grep -iE "getUserOpHash|failed to get UserOp hash|falling back to manual" | tail -20
```

**Expected**: Should see successful `getUserOpHash` calls, no "falling back to manual" warnings.

### 3.2: Check UserOp Hash Matches Frontend

```bash
# Check UserOp submission and hash calculation
sudo journalctl -u flow-evm-gateway --since "2 minutes ago" | \
  grep -iE "received eth_sendUserOperation|userOpHash|user operation added to pool" | tail -30
```

**Expected**: 
- UserOp hash should match frontend's hash: `0xd168d1d2dab37216e982701eb9eef29178378a4b29cf2c3ca975211766d54fb8`
- No hash mismatch warnings

### 3.3: Check for AA24 Errors (Should Be Gone)

```bash
# Check for AA24 signature errors
sudo journalctl -u flow-evm-gateway --since "2 minutes ago" | \
  grep -iE "AA24|signature error|hash.*mismatch" | tail -30
```

**Expected**: 
- **No AA24 errors** (or if present, they should be for different reasons, not hash mismatch)
- No "hash mismatch" warnings

### 3.4: Check Successful UserOp Processing

```bash
# Check for successful UserOp execution
sudo journalctl -u flow-evm-gateway --since "2 minutes ago" | \
  grep -iE "indexed.*UserOperation|success.*true|UserOperationEvent.*success" | tail -30
```

**Expected**: Should see successful UserOp events with `success: true`

### 3.5: Comprehensive UserOp Flow Check

```bash
# Full UserOp flow: submission → validation → bundling → execution
sudo journalctl -u flow-evm-gateway --since "2 minutes ago" | \
  grep -iE "userop|user.*operation" | tail -50
```

**Expected Flow**:
1. `received eth_sendUserOperation request` - UserOp submitted
2. `user operation added to pool` - Hash calculated (should match frontend)
3. `found pending UserOperations` - Bundler picks it up
4. `created handleOps transaction` - Transaction created
5. `submitted bundled transaction` - Transaction submitted
6. `indexed.*UserOperation.*success.*true` - UserOp executed successfully

### 3.6: Check Hash Calculation Consistency

```bash
# Compare hashes across different components
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | \
  grep -iE "userOpHash|expectedUserOpHash" | \
  awk '{print $NF}' | sort | uniq -c | sort -rn
```

**Expected**: All hashes should be the same (the frontend's hash: `0xd168d1d2dab37216e982701eb9eef29178378a4b29cf2c3ca975211766d54fb8`)

### 3.7: Monitor Real-Time (After Submitting a UserOp)

```bash
# Watch logs in real-time, filter for UserOp activity
sudo journalctl -u flow-evm-gateway -f | \
  grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion" | \
  grep -iE "userop|sendUserOperation|getUserOpHash|userOpHash|AA24|signature|hash|bundler|handleOps|success"
```

## Step 4: Verify Hash Match with Frontend

After submitting a UserOp from the frontend, check:

```bash
# Get the UserOp hash from gateway logs
GATEWAY_HASH=$(sudo journalctl -u flow-evm-gateway --since "1 minute ago" | \
  grep -i "userOpHash" | tail -1 | grep -oE "0x[a-fA-F0-9]{64}" | head -1)

echo "Gateway hash: $GATEWAY_HASH"
echo "Frontend hash: 0xd168d1d2dab37216e982701eb9eef29178378a4b29cf2c3ca975211766d54fb8"

# They should match!
```

## What Changed

### Fix: Empty Signature in getUserOpHash

**Before**:
```go
packedOp := PackedUserOperationABI{
    // ... other fields ...
    Signature: userOp.Signature,  // ❌ Wrong: includes actual signature
}
```

**After**:
```go
packedOp := PackedUserOperationABI{
    // ... other fields ...
    Signature: []byte{},  // ✅ Correct: empty signature
}
```

### Why This Matters

1. **ERC-4337 Spec**: The UserOp hash is what gets signed. The signature signs the hash, so the hash cannot include the signature itself.
2. **Frontend Match**: Frontend calls `EntryPoint.getUserOpHash()` with empty signature, so gateway must do the same.
3. **AA24 Fix**: Hash mismatch was causing AA24 signature errors. Now both frontend and gateway calculate the same hash.

## Expected Results

✅ **UserOp hash matches frontend**: `0xd168d1d2dab37216e982701eb9eef29178378a4b29cf2c3ca975211766d54fb8`  
✅ **No AA24 signature errors** (unless for other reasons)  
✅ **Successful UserOp execution**  
✅ **No "falling back to manual calculation" warnings**  
✅ **Consistent hash across all components** (pool, bundler, ingestion)

## Troubleshooting

### If hashes still don't match:

1. **Check EntryPoint address**:
   ```bash
   sudo journalctl -u flow-evm-gateway --since "1 minute ago" | grep -i "entryPoint"
   ```
   Should be: `0xCf1e8398747A05a997E8c964E957e47209bdFF08`

2. **Check chainID**:
   ```bash
   sudo journalctl -u flow-evm-gateway --since "1 minute ago" | grep -i "chainID"
   ```
   Should be: `545` for flow-testnet

3. **Verify getUserOpHash is being called**:
   ```bash
   sudo journalctl -u flow-evm-gateway --since "1 minute ago" | \
     grep -iE "failed to get UserOp hash|falling back"
   ```
   Should see no "falling back" warnings

### If AA24 errors persist:

1. Check signature v value (should be 0 or 1 for SimpleAccount, not 27/28)
2. Verify signature was signed over the correct hash
3. Check chainID matches (545 for flow-testnet)
4. Verify signature format is correct (65 bytes)

## Quick Reference

- **VERSION**: `no-keys-logging-getuserophash-fix`
- **EC2 IP**: `ec2-3-150-43-95.us-east-2.compute.amazonaws.com`
- **Config**: `/etc/flow/runtime-conf.env`
- **Expected Hash**: `0xd168d1d2dab37216e982701eb9eef29178378a4b29cf2c3ca975211766d54fb8`

