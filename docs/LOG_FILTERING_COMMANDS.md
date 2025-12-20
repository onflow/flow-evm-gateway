# Log Filtering Commands

## Filter Out All Noisy Logs

This command shows all logs except the constantly updating ones (new blocks, ingestion, etc.):

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock"
```

## Show Only UserOperation-Related Logs

For debugging UserOperation issues specifically:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion" | grep -iE "userop|sendUserOperation|simulateValidation|entrypoint|ownerFromInitCode|recoveredSigner|signerMatchesOwner|validation.*reverted|rawFactoryAddress|rawFunctionSelector|factoryAddress|functionSelector|initCodeHex|signature|revert"
```

## Show Only API and Validation Logs

For seeing API requests and validation results:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -iE "api|validation|error|userop|sendUserOperation|simulateValidation|entrypoint"
```

## Show Only Errors and Warnings

For seeing only errors and warnings:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock" | grep -iE "error|warn|fail|revert"
```

## Show Recent Logs (Last 100 Lines) Without Noise

To see recent logs without following:

```bash
sudo journalctl -u flow-evm-gateway -n 100 --no-pager | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock"
```

## Show Logs for Specific Time Range

To see logs from the last 10 minutes:

```bash
sudo journalctl -u flow-evm-gateway --since "10 minutes ago" | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock"
```

## Most Useful Command (Recommended)

This is the best command for general debugging - shows everything except block/ingestion noise:

```bash
sudo journalctl -u flow-evm-gateway -f | grep -vE "new evm block executed event|received new cadence evm events|received \`NotifyBlock\`|ingesting new transaction|component.*ingestion|new evm block|block.*height|block.*number|evm.*block|NotifyBlock"
```

## What Gets Filtered Out

The following patterns are excluded:
- `new evm block executed event` - Block execution events
- `received new cadence evm events` - Cadence event ingestion
- `received \`NotifyBlock\`` - Block notifications
- `ingesting new transaction` - Transaction ingestion
- `component.*ingestion` - Any ingestion component logs
- `new evm block` - General block messages
- `block.*height` - Block height updates
- `block.*number` - Block number updates
- `evm.*block` - EVM block messages
- `NotifyBlock` - Block notifications

## What You'll See

With the filtering, you'll see:
- API requests and responses
- UserOperation validation logs
- EntryPoint calls and results
- Signature recovery logs
- Error messages
- Warning messages
- initCode parsing logs
- Any other non-ingestion activity

