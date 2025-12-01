# Troubleshooting Empty Logs

## If You're Not Seeing Any Logs

### 1. Check if Gateway is Running

```bash
sudo systemctl status flow-evm-gateway
```

**Expected:** `active (running)`

### 2. Check if New Code is Deployed

```bash
sudo journalctl -u flow-evm-gateway -n 50 --no-pager | grep -i "version"
```

**Look for:** Version tag in logs (e.g., `"version":"testnet-v1-raw-initcode-logging"`)

If you see an old version, the new code hasn't been deployed yet.

### 3. Try Broader Filter (No Exclusions)

```bash
sudo journalctl -u flow-evm-gateway -f | grep -iE "userop|simulate|entrypoint|validation|revert"
```

This shows ALL logs matching those keywords, including block/ingestion logs.

### 4. Check All Recent Logs (Last 100 Lines)

```bash
sudo journalctl -u flow-evm-gateway -n 100 --no-pager
```

This shows the last 100 log lines without any filtering.

### 5. Check if UserOperations Are Being Sent

```bash
sudo journalctl -u flow-evm-gateway -f | grep -i "sendUserOperation\|eth_sendUserOperation"
```

**Expected:** Should see log entries when UserOps are sent

### 6. Check Log Level Settings

The gateway might be configured to only show Error level logs. Check configuration or try:

```bash
# Show all log levels
sudo journalctl -u flow-evm-gateway -f --no-pager
```

### 7. Most Permissive Filter (Shows Everything UserOp-Related)

```bash
sudo journalctl -u flow-evm-gateway -f | grep -i "user"
```

This catches:
- `userop`
- `userOp`
- `UserOperation`
- `SendUserOperation`
- etc.

### 8. Check for Any Errors

```bash
sudo journalctl -u flow-evm-gateway -f | grep -i "error"
```

### 9. Verify Service is Logging

```bash
# Check if service is producing logs at all
sudo journalctl -u flow-evm-gateway --since "1 minute ago" --no-pager
```

If this shows nothing, the service might not be running or not logging.

### 10. Check Docker Logs (if running in Docker)

```bash
# If running in Docker
sudo docker logs -f flow-evm-gateway 2>&1 | grep -iE "userop|simulate|entrypoint|validation|revert"
```

## Quick Diagnostic Commands

### See Everything (No Filtering)
```bash
sudo journalctl -u flow-evm-gateway -f
```

### See Last 50 Lines
```bash
sudo journalctl -u flow-evm-gateway -n 50 --no-pager
```

### See Logs from Last 5 Minutes
```bash
sudo journalctl -u flow-evm-gateway --since "5 minutes ago" --no-pager
```

### Check if Service Exists
```bash
sudo systemctl list-units | grep flow-evm
```

## Common Issues

1. **Service not running** → Start it: `sudo systemctl start flow-evm-gateway`
2. **New code not deployed** → Need to rebuild and redeploy
3. **No UserOperations being sent** → Check frontend/client
4. **Logs going to different location** → Check Docker logs or different service name
5. **Log level too high** → Only Error logs showing, Info/Debug filtered

## Next Steps

1. Run the diagnostic commands above
2. Check if service is running
3. Check if new code is deployed (version tag)
4. Try sending a UserOperation and watch logs
5. If still nothing, check Docker logs or service configuration

