# UserOperation Validation Process - Detailed Flow

## Overview

This document explains the complete flow of a UserOperation (UserOp) for account creation, from the client request through gateway validation to EntryPoint execution.

## 1. Client-Side Preparation

### Step 1.1: Build initCode

The client constructs the `initCode` that will create the smart account:

```
initCode = factoryAddress (20 bytes) + functionSelector (4 bytes) + ABI-encoded parameters
```

**Example:**

```
Factory Address: 0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12 (20 bytes)
Function Selector: 0x5fbfb9cf (4 bytes) - createAccount(address owner, uint256 salt)
Owner Address: 0x3cC530e139Dd93641c3F30217B20163EF8b17159 (32 bytes, ABI-encoded)
Salt: 0x0000000000000000000000000000000000000000000000000000000000000000 (32 bytes)

Total initCode: 20 + 4 + 32 + 32 = 88 bytes
```

**Hex representation:**

```
0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12  (factory, 20 bytes)
5fbfb9cf  (selector, 4 bytes)
0000000000000000000000003cc530e139dd93641c3f30217b20163ef8b17159  (owner, 32 bytes)
0000000000000000000000000000000000000000000000000000000000000000  (salt, 32 bytes)
```

### Step 1.2: Calculate UserOp Hash

The client calculates the hash that will be signed:

```
packedUserOp = keccak256(
    sender (20 bytes) ||
    nonce (32 bytes) ||
    keccak256(initCode) (32 bytes) ||
    keccak256(callData) (32 bytes) ||
    callGasLimit (32 bytes) ||
    verificationGasLimit (32 bytes) ||
    preVerificationGas (32 bytes) ||
    maxFeePerGas (32 bytes) ||
    maxPriorityFeePerGas (32 bytes) ||
    keccak256(paymasterAndData) (32 bytes)
)

userOpHash = keccak256(
    keccak256(packedUserOp) ||
    entryPoint (20 bytes) ||
    chainId (32 bytes)
)
```

**Result:** `0x632f83fafd5537eca5d485ceba8575c18527e4081d6ef16a187cf831bc1a8d82`

### Step 1.3: Sign UserOp Hash

The client signs the `userOpHash` using the owner's private key:

```
signature = ECDSA.sign(userOpHash, privateKey)
```

**Signature format:** `r (32 bytes) || s (32 bytes) || v (1 byte) = 65 bytes total`

**Note:** For ERC-4337, `v` should be 0 or 1 (recovery ID), but some libraries use 27/28 (EIP-155 format).

### Step 1.4: Build UserOperation Object

The client creates the complete UserOp:

```json
{
  "sender": "0x71ee4bc503BeDC396001C4c3206e88B965c6f860",
  "nonce": "0x0",
  "initCode": "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d125fbfb9cf0000000000000000000000003cc530e139dd93641c3f30217b20163ef8b171590000000000000000000000000000000000000000000000000000000000000000",
  "callData": "0x",
  "callGasLimit": "0x186a0",
  "verificationGasLimit": "0x186a0",
  "preVerificationGas": "0x5208",
  "maxFeePerGas": "0x3b9aca00",
  "maxPriorityFeePerGas": "0x3b9aca00",
  "paymasterAndData": "0x",
  "signature": "0x1d0eeb364b7997bcad9dd2e97ec381316e1baebfc918713140c74db244e848a114e9d057ebbf1bef53b9c33e2c334f0942a750a2b41fe857e4a484718c8380b81b"
}
```

### Step 1.5: Send to Gateway

The client sends the UserOp via JSON-RPC:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "eth_sendUserOperation",
  "params": [
    {
      "sender": "0x71ee4bc503BeDC396001C4c3206e88B965c6f860",
      "nonce": "0x0",
      "initCode": "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d125fbfb9cf...",
      ...
    },
    "0xcf1e8398747a05a997e8c964e957e47209bdff08"
  ]
}
```

---

## 2. Gateway Receives Request

### Step 2.1: JSON-RPC Parsing

The gateway receives the request and parses it:

1. **JSON unmarshaling** converts the hex strings to `hexutil.Bytes`:

   ```go
   type UserOperationArgs struct {
       InitCode *hexutil.Bytes `json:"initCode,omitempty"`
       ...
   }
   ```

2. **hexutil.Bytes** automatically:
   - Removes `0x` prefix
   - Decodes hex string to bytes
   - Stores as `[]byte`

**Expected result:**

- `initCode` should be exactly 88 bytes
- First 20 bytes: factory address
- Next 4 bytes: function selector
- Next 32 bytes: owner address (ABI-encoded)
- Last 32 bytes: salt

### Step 2.2: Log Raw InitCode (NEW)

The gateway logs the raw initCode as received:

```go
if userOpArgs.InitCode != nil {
    logFields = logFields.
        Int("initCodeLen", len(*userOpArgs.InitCode)).
        Str("initCodeHex", hexutil.Encode(*userOpArgs.InitCode))
    if len(*userOpArgs.InitCode) >= 24 {
        factoryAddr := common.BytesToAddress((*userOpArgs.InitCode)[0:20])
        selector := hexutil.Encode((*userOpArgs.InitCode)[20:24])
        logFields = logFields.
            Str("rawFactoryAddress", factoryAddr.Hex()).
            Str("rawFunctionSelector", selector)
    }
}
```

**Expected log:**

```json
{
  "initCodeLen": 88,
  "initCodeHex": "0x2e9f1433c8bc371c391b0f59c1e15da8affc9d125fbfb9cf0000000000000000000000003cc530e139dd93641c3f30217b20163ef8b171590000000000000000000000000000000000000000000000000000000000000000",
  "rawFactoryAddress": "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12",
  "rawFunctionSelector": "0x5fbfb9cf"
}
```

### Step 2.3: Convert to UserOperation

The gateway converts `UserOperationArgs` to `UserOperation`:

```go
func (args *UserOperationArgs) ToUserOperation() (*UserOperation, error) {
    uo := &UserOperation{
        Sender: args.Sender,
        ...
    }

    if args.InitCode != nil {
        uo.InitCode = *args.InitCode  // Direct assignment, should preserve bytes
    }

    return uo, nil
}
```

**Expected result:** `userOp.InitCode` should be identical to `*userOpArgs.InitCode` (88 bytes)

---

## 3. Gateway Validation

### Step 3.1: Check Account Existence

The gateway checks if the account already exists:

```go
accountCode, err := v.requester.GetCode(userOp.Sender, height)
if err == nil && len(accountCode) > 0 {
    // Account exists - EntryPoint will reject
} else {
    // Account doesn't exist - proceed with creation
}
```

**Expected:** Account doesn't exist (code length = 0)

### Step 3.2: Calculate UserOp Hash

The gateway calculates the UserOp hash using the same algorithm as the client:

```go
userOpHash, err := userOp.Hash(entryPoint, chainID)
```

**Expected:** `0x632f83fafd5537eca5d485ceba8575c18527e4081d6ef16a187cf831bc1a8d82`

**Verification:** Should match client's hash exactly.

### Step 3.3: Extract Owner from initCode

The gateway extracts the owner address from initCode:

```go
func extractOwnerFromInitCode(initCode []byte) (common.Address, error) {
    // Owner is at bytes 36-55 (last 20 bytes of the 32-byte word starting at byte 24)
    ownerBytes := initCode[36:56]
    return common.BytesToAddress(ownerBytes), nil
}
```

**Expected:** `0x3cC530e139Dd93641c3F30217B20163EF8b17159`

### Step 3.4: Recover Signer from Signature

The gateway recovers the signer address from the signature:

```go
// Extract r, s, v from signature
r := userOp.Signature[0:32]
s := userOp.Signature[32:64]
sigV := userOp.Signature[64]

// Convert EIP-155 v (27/28) to recovery ID (0/1)
recoveryID := sigV
if sigV == 27 {
    recoveryID = 0
} else if sigV == 28 {
    recoveryID = 1
}

// Recover public key
pubKey, err := crypto.Ecrecover(userOpHash.Bytes(), append(r, append(s, recoveryID)...))
recoveredSigner := crypto.PubkeyToAddress(*pubKey)
```

**Expected:** `0x3cC530e139Dd93641c3F30217B20163EF8b17159`

**Verification:** `recoveredSigner == ownerFromInitCode` should be `true`

### Step 3.5: Log Processed InitCode

The gateway logs the initCode after processing:

```go
if len(userOp.InitCode) >= 24 {
    factoryAddr := common.BytesToAddress(userOp.InitCode[0:20])
    selector := hexutil.Encode(userOp.InitCode[20:24])
    v.logger.Info().
        Str("factoryAddress", factoryAddr.Hex()).
        Str("functionSelector", selector).
        Int("initCodeLen", len(userOp.InitCode)).
        Str("initCodeHex", hexutil.Encode(userOp.InitCode)).
        Msg("decoded initCode details")
}
```

**Expected log:**

```json
{
  "factoryAddress": "0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12",
  "functionSelector": "0x5fbfb9cf",
  "initCodeLen": 88,
  "initCodeHex": "0x2e9f1433c8bc371c391b0f59c1e15da8affc9d125fbfb9cf..."
}
```

**Verification:** Should match raw values from Step 2.2

### Step 3.6: Encode for EntryPoint

The gateway encodes the UserOp for EntryPoint's `simulateValidation`:

```go
func EncodeSimulateValidation(userOp *models.UserOperation) ([]byte, error) {
    op := struct {
        Sender               common.Address
        Nonce                *big.Int
        InitCode             []byte  // Direct assignment
        CallData             []byte
        ...
    }{
        Sender:   userOp.Sender,
        Nonce:    userOp.Nonce,
        InitCode: userOp.InitCode,  // Should be 88 bytes
        ...
    }

    // ABI encode
    data, err := entryPointABIParsed.Pack("simulateValidation", op)
    return data, nil
}
```

**ABI Encoding for `bytes` field:**

- Offset to data (32 bytes): `0x0000000000000000000000000000000000000000000000000000000000000160` (352 bytes)
- At offset 352: Length (32 bytes): `0x0000000000000000000000000000000000000000000000000000000000000058` (88 bytes)
- At offset 384: Data (88 bytes): The actual initCode bytes

**Expected calldata structure:**

```
0xee219423  (function selector: simulateValidation)
... (UserOp struct fields) ...
0x0000000000000000000000000000000000000000000000000000000000000160  (initCode offset)
... (other fields) ...
0x0000000000000000000000000000000000000000000000000000000000000058  (initCode length: 88)
2e9f1433c8bc371c391b0f59c1e15da8affc9d12  (factory, 20 bytes)
5fbfb9cf  (selector, 4 bytes)
0000000000000000000000003cc530e139dd93641c3f30217b20163ef8b17159  (owner, 32 bytes)
0000000000000000000000000000000000000000000000000000000000000000  (salt, 32 bytes)
```

---

## 4. EntryPoint.simulateValidation

### Step 4.1: EntryPoint Receives Call

EntryPoint receives the `simulateValidation` call with the encoded UserOp.

### Step 4.2: EntryPoint Decodes UserOp

EntryPoint decodes the ABI-encoded UserOp struct:

- Extracts `initCode` bytes field (should be 88 bytes)
- Extracts all other UserOp fields

### Step 4.3: EntryPoint Creates Account (if needed)

If `initCode` is non-empty and account doesn't exist:

1. **Extract factory address** from `initCode[0:20]`
2. **Extract function call** from `initCode[20:]` (selector + params)
3. **Call factory via senderCreator**:

   ```solidity
   address senderCreator = entryPoint.senderCreator();
   // senderCreator calls factory.createAccount(owner, salt)
   SimpleAccount account = ISenderCreator(senderCreator).createSender(initCode);
   ```

4. **Factory.createAccount execution**:

   ```solidity
   function createAccount(address owner, uint256 salt) public returns (SimpleAccount ret) {
       require(msg.sender == address(senderCreator), "NotSenderCreator");

       address addr = getAddress(owner, salt);
       if (addr.code.length > 0) {
           return SimpleAccount(payable(addr));  // Already exists
       }

       // Create new account via CREATE2
       ret = SimpleAccount(payable(new ERC1967Proxy{salt: bytes32(salt)}(
           address(accountImplementation),
           abi.encodeCall(SimpleAccount.initialize, (owner))
       )));
   }
   ```

5. **Account initialization**:
   ```solidity
   function initialize(address anOwner) public initializer {
       owner = anOwner;
       emit SimpleAccountInitialized(entryPoint(), owner);
   }
   ```

**Expected result:** Account is created at `0x71ee4bc503BeDC396001C4c3206e88B965c6f860` with owner `0x3cC530e139Dd93641c3F30217B20163EF8b17159`

### Step 4.4: EntryPoint Validates Signature

EntryPoint calls the account's `validateUserOp`:

```solidity
function _validateSignature(PackedUserOperation calldata userOp, bytes32 userOpHash)
    internal override virtual returns (uint256 validationData)
{
    if (owner != ECDSA.recover(userOpHash, userOp.signature)) {
        return SIG_VALIDATION_FAILED;
    }
    return SIG_VALIDATION_SUCCESS;
}
```

**What happens:**

1. EntryPoint passes the `userOpHash` and `signature` to the account
2. Account recovers signer using `ECDSA.recover(userOpHash, signature)`
3. Account compares recovered signer to `owner`
4. If match: returns `SIG_VALIDATION_SUCCESS`
5. If mismatch: returns `SIG_VALIDATION_FAILED`

**Expected:** `SIG_VALIDATION_SUCCESS` (recovered signer == owner)

### Step 4.5: EntryPoint Returns (or Reverts)

If validation succeeds:

- EntryPoint returns successfully (no revert)
- Gateway accepts the UserOp

If validation fails:

- EntryPoint reverts with error
- Gateway rejects the UserOp

---

## 5. Current Issue

### Problem

EntryPoint is reverting with **empty revert reason**, which indicates:

1. **Account creation failed**: Factory call failed (wrong address, wrong selector, etc.)
2. **Signature validation failed**: Recovered signer doesn't match owner
3. **EntryPoint internal validation failed**: Some other EntryPoint check failed

### Debugging Steps

1. **Verify raw initCode** (Step 2.2):

   - Check `rawFactoryAddress` matches expected
   - Check `rawFunctionSelector` matches expected
   - Check `initCodeLen` is 88

2. **Verify processed initCode** (Step 3.5):

   - Check `factoryAddress` matches raw value
   - Check `functionSelector` matches raw value
   - Check `initCodeHex` matches raw value

3. **Verify signature recovery** (Step 3.4):

   - Check `recoveredSigner` matches `ownerFromInitCode`
   - Check `signerMatchesOwner` is `true`

4. **Verify calldata** (Step 3.6):

   - Check `calldataHex` contains correct initCode
   - Check initCode in calldata matches processed initCode

5. **Use debug_traceCall**:
   - Trace EntryPoint execution to see where it fails
   - Check if factory is called correctly
   - Check if account is created
   - Check if signature validation is called

---

## 6. Expected Log Sequence

### Successful Flow

```
1. [userop-api] received eth_sendUserOperation request
   - initCodeLen: 88
   - rawFactoryAddress: 0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12
   - rawFunctionSelector: 0x5fbfb9cf

2. [userop-validator] account does not exist yet - proceeding with account creation

3. [userop-validator] decoded initCode details
   - factoryAddress: 0x2e9f1433C8bC371C391b0F59c1e15Da8AFfC9d12
   - functionSelector: 0x5fbfb9cf
   - ownerFromInitCode: 0x3cC530e139Dd93641c3F30217B20163EF8b17159

4. [userop-validator] signature recovery succeeded
   - recoveredSigner: 0x3cC530e139Dd93641c3F30217B20163EF8b17159
   - signerMatchesOwner: true

5. [userop-validator] calling EntryPoint.simulateValidation with full UserOp details
   - calldataHex: 0xee219423...
   - calldataLen: 708

6. [userop-validator] EntryPoint.simulateValidation succeeded (no error)

7. [userop-api] user operation added to pool
```

### Failed Flow (Current)

```
1-4. Same as above ✅

5. [userop-validator] calling EntryPoint.simulateValidation with full UserOp details
   - calldataHex: 0xee219423...
   - calldataLen: 708

6. [userop-validator] EntryPoint.simulateValidation call failed
   - error: execution reverted
   - revertReasonHex: 0x
   - revertDataLen: 0

7. [userop-api] user operation validation failed
```

---

## 7. Key Verification Points

1. **Raw initCode** (from RPC): Should be exactly 88 bytes with correct factory address
2. **Processed initCode** (after ToUserOperation): Should match raw initCode exactly
3. **UserOp hash**: Should match between client and gateway
4. **Signature recovery**: Should recover the owner address
5. **Calldata initCode**: Should match processed initCode
6. **EntryPoint execution**: Should create account and validate signature

Any mismatch at any step indicates where the issue is occurring.
