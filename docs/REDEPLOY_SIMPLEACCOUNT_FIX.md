# Quick Fix: Redeploy SimpleAccount Without Proxy

## The Problem

Your current SimpleAccountFactory deploys accounts using ERC1967Proxy, which causes:
- `msg.sender` = proxy address (not EntryPoint)
- SimpleAccount's `_requireForExecute()` fails → AA23 error

## Quick Fix: Deploy SimpleAccount Directly

### Option 1: Modify SimpleAccountFactory (Recommended)

Update your `SimpleAccountFactory.sol` to deploy SimpleAccount directly:

```solidity
// Modified SimpleAccountFactory.sol
function createAccount(address owner, uint256 salt) public returns (SimpleAccount ret) {
    require(msg.sender == address(senderCreator), "NotSenderCreator");
    
    address addr = getAddress(owner, salt);
    uint256 codeSize = addr.code.length;
    if (codeSize > 0) {
        return SimpleAccount(payable(addr));
    }
    
    // Deploy SimpleAccount directly (no proxy)
    ret = new SimpleAccount{salt: bytes32(salt)}(entryPoint());
    ret.initialize(owner);
}

function getAddress(address owner, uint256 salt) public virtual view returns (address) {
    // Calculate CREATE2 address for SimpleAccount directly
    return Create2.computeAddress(
        bytes32(salt),
        keccak256(abi.encodePacked(
            type(SimpleAccount).creationCode,
            abi.encode(entryPoint())  // Constructor parameter
        ))
    );
}
```

**Changes:**
- Removed `ERC1967Proxy` deployment
- Deploy `SimpleAccount` directly with `new SimpleAccount{salt: ...}(entryPoint())`
- Updated `getAddress()` to calculate address for direct deployment

### Option 2: Use Production Account Factory

Deploy ZeroDev or Alchemy account factory instead - they handle proxies correctly.

## Deployment Steps

1. **Update SimpleAccountFactory contract** (remove proxy pattern)
2. **Deploy new factory** to Flow testnet
3. **Update frontend** to use new factory address
4. **Create new account** - will work correctly with gateway

## Verification

After redeploying, test with a UserOp:
- Account should be created successfully
- `validateUserOp()` should pass (msg.sender == EntryPoint)
- `execute()` should succeed
- No AA23 errors

## Gateway Status

✅ **Gateway requires NO changes** - it will work automatically once account is deployed correctly.

The gateway is account-agnostic and works with any ERC-4337 compatible account deployment pattern.

