# SenderCreator Diagnostic Instructions (Updated for Current EntryPoint)

## ⚠️ Important: EntryPoint Address Update

The gateway is currently configured to use the **new EntryPoint address**:

- **Current EntryPoint**: `0x33860348CE61eA6CeC276b1cF93C5465D1a92131` (v0.9.0)

The contract agent's instructions reference the old EntryPoint address. Use the addresses below for diagnostics.

---

## Contract Addresses (Flow Testnet - Current)

- **EntryPoint**: `0x33860348CE61eA6CeC276b1cF93C5465D1a92131` ⚠️ **UPDATED**
- **SimpleAccountFactory**: `0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f` ⚠️ **UPDATED**
- **SenderCreator**: `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2` ⚠️ **UPDATED** (returned by current EntryPoint)

---

## Diagnostic Commands (Updated Addresses)

### 1. Verify RPC Endpoint Configuration

```bash
curl -X POST https://testnet.evm.nodes.onflow.org \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"eth_chainId",
    "params":[]
  }'
```

**Expected:** `{"result":"0x221"}` (545 in hex)

---

### 2. Test senderCreator() Directly (Updated EntryPoint Address)

```bash
curl -X POST https://testnet.evm.nodes.onflow.org \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"eth_call",
    "params":[{
      "to": "0x33860348CE61eA6CeC276b1cF93C5465D1a92131",
      "data": "0x4af63f02"
    }, "latest"]
  }'
```

**Function Selector:** `0x4af63f02` = `keccak256("senderCreator()")[0:4]`

**Expected Result:**

- Should return a 32-byte address (64 hex characters + `0x`)
- **Current SenderCreator**: `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2`
- Example: `{"result":"0x000000000000000000000000645fb1402f9ab66dbfa96997304577f30cc6b6d2"}`

**Note:** Different EntryPoint deployments have different SenderCreator addresses. The current EntryPoint returns `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2`, which is correct for this deployment.

---

### 3. Check Node Sync Status

```bash
curl -X POST https://testnet.evm.nodes.onflow.org \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"eth_blockNumber",
    "params":[]
  }'
```

Compare with: https://evm-testnet.flowscan.io

---

### 4. Verify EntryPoint Code (Updated Address)

```bash
curl -X POST https://testnet.evm.nodes.onflow.org \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"eth_getCode",
    "params":["0x33860348CE61eA6CeC276b1cF93C5465D1a92131", "latest"]
  }'
```

**Expected:**

- Non-empty result (should be substantial bytecode)
- If empty or different, the node is pointing to wrong network or address

---

### 5. Verify SenderCreator Contract Exists

**Current SenderCreator Address**: `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2`

Check if it has code:

```bash
curl -X POST https://testnet.evm.nodes.onflow.org \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"eth_getCode",
    "params":["0x645fb1402f9AB66DbfA96997304577F30cC6B6D2", "latest"]
  }'
```

**Expected:** Non-empty result (should have bytecode)

**Why this is better:**

- Different EntryPoint deployments have different SenderCreator addresses
- The current EntryPoint (`0x33860348CE61eA6CeC276b1cF93C5465D1a92131`) returns `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2`, which is correct for this deployment
- The diagnostic now verifies the actual deployment rather than comparing to an outdated address

---

## Gateway Configuration Check

Verify the gateway is using the correct EntryPoint address:

```bash
# Check gateway logs for EntryPoint address
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" | grep -i "entrypoint\|entryPoint"
```

**Expected:** Should show `0x33860348CE61eA6CeC276b1cF93C5465D1a92131`

---

## Quick Diagnostic Checklist

Run these in order:

- [ ] ✅ RPC endpoint is correct (Chain ID 545)
- [ ] ✅ Node is fully synced
- [ ] ✅ EntryPoint has code at `0x33860348CE61eA6CeC276b1cF93C5465D1a92131` ⚠️ **UPDATED**
- [ ] ✅ Direct `eth_call` to `senderCreator()` succeeds (using new EntryPoint address)
- [ ] ✅ SenderCreator address matches: `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2`
- [ ] ✅ SenderCreator has code at `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2`
- [ ] ✅ Gateway is configured with correct EntryPoint address
- [ ] ✅ `senderCreator()` works during UserOperation simulation
- [ ] ✅ Account creation flow completes successfully

---

## If All Checks Pass But Still Failing

**Possible causes:**

1. **Address mismatch** - Gateway using different EntryPoint than expected
2. **Caching issue** - Gateway might be caching old/stale state
3. **State override** - If using state overrides, ensure EntryPoint state is correct
4. **Gas estimation** - Ensure sufficient gas for `senderCreator()` call
5. **Transaction context** - Check if issue only occurs in specific transaction contexts

**Next steps:**

1. Verify gateway configuration matches current EntryPoint address
2. Clear any caches
3. Try with fresh state (no overrides)
4. Increase gas limits
5. Test in isolation (direct call vs. in transaction)

---

## Reference

- **Current EntryPoint**: `0x33860348CE61eA6CeC276b1cF93C5465D1a92131` (v0.9.0)
- **SimpleAccountFactory**: `0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f` ⚠️ **UPDATED**
- **SenderCreator**: `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2` ⚠️ **UPDATED** (returned by current EntryPoint)
- **Gateway Config**: `config/config.go` line 136
- **Frontend Hash Fix**: `docs/FRONTEND_HASH_FIX.md` (uses current EntryPoint address)

## Gateway Implementation Note

✅ **The gateway already uses the correct SenderCreator address** - it dynamically fetches it from `EntryPoint.senderCreator()` at runtime, so it automatically uses `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2` for the current EntryPoint deployment. No code changes needed.
