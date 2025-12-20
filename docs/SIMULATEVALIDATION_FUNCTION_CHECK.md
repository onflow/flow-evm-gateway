# simulateValidation Function Existence Check

## Problem

The gateway was treating empty reverts from `simulateValidation` as "expected behavior", but empty reverts (0x, length 0) actually indicate that **the function doesn't exist** on the EntryPoint.

When a function doesn't exist:
1. The call falls through to the fallback function
2. The fallback reverts with no data (`revert()` or `revert(0,0)`)
3. This results in an empty revert (`0x`, length 0)

## Solution

Added detection to check if `simulateValidation` actually exists on the EntryPoint by:
1. **Extracting the function selector** from the calldata (first 4 bytes)
2. **Fetching EntryPoint bytecode** using `GetCode()`
3. **Searching for the selector** in the bytecode
4. **Logging clear error messages** if the function doesn't exist

## Changes Made

### 1. Added `bytes` Import

```go
import (
    "bytes"
    ...
)
```

### 2. Enhanced Empty Revert Detection

When `revertData` is empty, the gateway now:
- Checks if the function selector exists in EntryPoint bytecode
- If selector **not found**: Logs error that function doesn't exist
- If selector **found but still empty revert**: Logs warning (unusual case)
- If bytecode check **fails**: Logs warning but treats as failure

### 3. Clear Error Messages

**Function doesn't exist:**
```
"simulateValidation function does not exist on this EntryPoint (selector not found in bytecode). Empty revert indicates function call fell through to fallback. This EntryPoint may not support simulateValidation or may use a different simulation method."
```

**Selector exists but empty revert:**
```
"simulateValidation selector exists in EntryPoint bytecode but reverted with empty data. This may indicate a different EntryPoint version or implementation issue."
```

## What This Reveals

After deployment, when you send a UserOperation, you'll see one of:

1. **Function doesn't exist:**
   - `"functionSelector": "0xee219423"`
   - `"entryPointCodeLen": <number>`
   - `"simulateValidation function does not exist on this EntryPoint"`
   - This means EntryPoint doesn't have `simulateValidation` - may need separate simulation contract

2. **Function exists but empty revert:**
   - `"functionSelector": "0xee219423"`
   - `"simulateValidation selector exists in EntryPoint bytecode but reverted with empty data"`
   - This is unusual - function exists but something else is wrong

3. **Couldn't check bytecode:**
   - `"Could not verify if simulateValidation function exists"`
   - Treats as failure for safety

## Next Steps

1. **Deploy and test** - See which case you hit
2. **If function doesn't exist:**
   - Check if Flow uses a separate `EntryPointSimulations` contract
   - Check if EntryPoint uses `handleOps` with state overrides instead
   - Verify the actual EntryPoint ABI/bytecode
3. **If function exists:**
   - Investigate why it's reverting with empty data
   - Check EntryPoint version compatibility
   - Verify EntryPoint implementation matches expected behavior

## Diagnostic Command

After deployment, watch for:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -E "simulateValidation function does not exist|selector exists in EntryPoint bytecode|functionSelector|entryPointCodeLen"
```

