# Current Contract Addresses (Flow Testnet)

## ⚠️ Official Addresses (Updated)

These are the **current, correct addresses** for Flow Testnet:

### EntryPoint (v0.9.0)
- **Address**: `0x33860348CE61eA6CeC276b1cF93C5465D1a92131`
- **Explorer**: https://evm-testnet.flowscan.io/address/0x33860348CE61eA6CeC276b1cF93C5465D1a92131
- **Version**: v0.9.0
- **Purpose**: ERC-4337 EntryPoint contract for UserOperation processing

### SimpleAccountFactory (v0.9.0)
- **Address**: `0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f`
- **Explorer**: https://evm-testnet.flowscan.io/address/0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f
- **Version**: v0.9.0
- **Purpose**: Factory contract for creating SimpleAccount instances

### SenderCreator
- **Address**: `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2`
- **Explorer**: https://evm-testnet.flowscan.io/address/0x645fb1402f9AB66DbfA96997304577F30cC6B6D2
- **Purpose**: Helper contract for account creation during UserOperation execution
- **Note**: This address is returned by `EntryPoint.senderCreator()` for the current EntryPoint deployment. Different EntryPoint deployments have different SenderCreator addresses.

---

## Environment Variables

For frontend/backend configuration:

```bash
# EntryPoint
NEXT_PUBLIC_ENTRY_POINT_ADDRESS=0x33860348ce61ea6cec276b1cf93c5465d1a92131

# SimpleAccountFactory
NEXT_PUBLIC_SIMPLE_ACCOUNT_FACTORY_ADDRESS=0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f
```

---

## Gateway Configuration

The gateway is configured with these addresses in:
- **Config File**: `config/config.go` line 136
- **Command Line**: `--entry-point-address=0x33860348ce61ea6cec276b1cf93c5465d1a92131`

---

## Deprecated Addresses

⚠️ **Do not use these addresses** - they are outdated:

- **Old EntryPoint**: `0xcf1e8398747a05a997e8c964e957e47209bdff08` (deprecated)
- **Old SimpleAccountFactory**: `0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12` (deprecated)

---

## Verification

To verify these addresses are correct:

1. **Check EntryPoint code**:
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

2. **Check SimpleAccountFactory code**:
   ```bash
   curl -X POST https://testnet.evm.nodes.onflow.org \
     -H 'Content-Type: application/json' \
     --data '{
       "jsonrpc":"2.0",
       "id":1,
       "method":"eth_getCode",
       "params":["0x246C8f6290be97ebBa965846eD9AE0F0BE6a360f", "latest"]
     }'
   ```

3. **Check EntryPoint senderCreator**:
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
   **Expected**: Should return `0x645fb1402f9AB66DbfA96997304577F30cC6B6D2`

4. **Verify SenderCreator has code**:
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
   **Expected**: Non-empty result (should have bytecode)

---

## References

- **Frontend Hash Fix**: `docs/FRONTEND_HASH_FIX.md`
- **SenderCreator Diagnostics**: `docs/SENDERCREATOR_DIAGNOSTICS.md`
- **Gateway Config**: `config/config.go`

