# Updated Understanding - EntryPoint v0.9 Behavior

## Critical Corrections

### 1. simulateValidation is SUPPOSED to Revert ✅

**Correct Understanding:**
- ✅ In ERC-4337, simulation functions **always revert** with structured data
- ✅ The revert data contains the result (gas estimates, validation status)
- ✅ We must decode the revert payload, not look for success returns
- ✅ "Revert" is not a bug - it's the design

**What We Need to Do:**
- Decode revert data as `ValidationResult` struct or `FailedOp` errors
- Handle revert as expected behavior, not failure
- Extract gas estimates from revert data

### 2. senderCreator() Exists in v0.9 ✅

**Correct Understanding:**
- ✅ Official v0.9 release notes: **"Make SenderCreator address public (AA-470)… Now this address is exposed by the senderCreator() function."**
- ✅ `senderCreator()` is a **public view function** in v0.9
- ✅ If our call reverts, it means:
  - Wrong EntryPoint address/chain
  - Wrong ABI (from different version)
  - Custom build from older commit

**What We Need to Do:**
- Verify EntryPoint codehash matches official v0.9
- Use correct ABI from exact v0.9 commit
- Add `senderCreator()` to our ABI

### 3. "Empty Revert" = Custom Errors Not Decoded ❌

**Correct Understanding:**
- ✅ EntryPoint uses custom errors: `FailedOp(uint256,string)`, AAxx errors
- ✅ We must decode revert data according to EntryPoint ABI
- ✅ "Empty reason" means we're not decoding custom errors

**What We Need to Do:**
- Decode `FailedOp(uint256,string)` errors
- Handle AAxx error codes (AA10, AA13, AA20, AA23, etc.)
- Decode `ValidationResult` struct from revert data
- Log full revert data, not just reason string

## Updated Gateway Status

### ✅ Gateway UserOp Handling is Correct

**Verified:**
- UserOp construction: ✅ Correct
- ABI encoding: ✅ Correct
- Hash calculation: ✅ Correct
- Signature recovery: ✅ Correct
- Calldata structure: ✅ Correct

### ❌ Gateway Revert Decoding Needs Improvement

**Current Issues:**
- We decode `FailedOp` but might miss `ValidationResult` struct
- We might not be getting full revert data
- We need to add EntryPoint error definitions to ABI
- We need to handle AAxx error codes

### ❌ EntryPoint Version/ABI May Be Mismatched

**Likely Issues:**
- EntryPoint ABI missing `senderCreator()` function
- EntryPoint ABI missing error definitions
- Need to verify codehash matches official v0.9

## Next Steps

1. **Update EntryPoint ABI:**
   - Add `senderCreator()` function
   - Add error definitions (`FailedOp`, `FailedOpWithRevert`)
   - Verify against official v0.9 ABI

2. **Improve Revert Decoding:**
   - Decode `ValidationResult` struct
   - Handle AAxx error codes
   - Ensure we get full revert data

3. **Verify EntryPoint Version:**
   - Check codehash against official v0.9
   - Verify EntryPoint address
   - Re-generate ABI from exact commit

4. **Update Gateway Logic:**
   - Don't treat revert as failure - decode revert data
   - Handle `ValidationResult` as success (with gas estimates)
   - Only fail on actual validation errors (AAxx codes)

## Summary

**The gateway UserOp handling is correct. The issues are:**
1. EntryPoint version/ABI mismatch (missing `senderCreator()`, error definitions)
2. Not decoding revert data properly (`ValidationResult`, AAxx errors)
3. Treating revert as failure instead of expected behavior

**The real failure is likely:**
- Factory call failing (AA13)
- Gas/prefund issues (AA21/AA22/AA23)
- Account already exists (AA10)
- Signature validation (AA23)

**All of which we'll see once we decode revert data properly.**

