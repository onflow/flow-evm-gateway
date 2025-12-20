# Signature Format Analysis

## Problem

EntryPoint is reverting with empty data. The signature format might be the issue.

## Key Observations

1. **Signature hex in logs**: Ends with `...800` (0x00 = v=0)
2. **Calldata hex**: Ends with `...1b` (0x1b = 27)
3. **Log shows**: `signatureV: 0` but signature hex shows `...800`

## Signature Format Requirements

### SimpleAccount Expects

- **v = 0 or 1** (recovery ID format)
- NOT v = 27 or 28 (EIP-155 format)

### What Client is Sending

- From logs: `signatureV: 27` in recovery log
- But `signatureV: 0` in EntryPoint call log
- Signature hex: `...800` (v=0)
- Calldata: `...1b` (v=27)

## The Issue

There's a **discrepancy**:

- The signature being sent to EntryPoint has `v=0` (from signature hex)
- But the calldata shows `v=27` (from calldata hex)

This suggests the signature might be getting modified somewhere, OR the calldata encoding is different from the raw signature.

## Next Steps

1. **Check if SimpleAccount expects v=0/1**: Yes, it does
2. **Check what the client is actually sending**: Need to verify from frontend
3. **Check if EntryPoint is rejecting due to signature format**: Likely

## Solution

The frontend should send `v=0` or `v=1` (recovery ID), not `v=27` or `v=28` (EIP-155 format).

If the client is sending `v=27`, SimpleAccount will reject it because it expects recovery ID format (0/1), not EIP-155 format (27/28).

## How to Verify

Check the frontend code to see what `v` value is being sent in the signature. It should be:

- `v = 0` or `v = 1` for SimpleAccount
- NOT `v = 27` or `v = 28`
