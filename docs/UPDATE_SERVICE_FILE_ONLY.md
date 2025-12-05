# Update Service File Only - No Rebuild Needed

## Quick Answer

**No rebuild/redeploy needed** - Just update the systemd service file on your server and restart.

## Steps

### 1. Create Database Directory (if it doesn't exist)

```bash
sudo mkdir -p /data/evm-gateway
sudo chown $USER:$USER /data/evm-gateway  # Or appropriate user
```

### 2. Update Service File on Server

Copy the updated service file to your server, or manually edit it:

```bash
# Option A: Copy from your local machine (if you have the updated file)
scp deploy/systemd-docker/flow-evm-gateway.service user@your-server:/tmp/
ssh user@your-server "sudo cp /tmp/flow-evm-gateway.service /etc/systemd/system/"

# Option B: Edit directly on server
sudo nano /etc/systemd/system/flow-evm-gateway.service
```

**Add this line** after `--name flow-evm-gateway \`:
```ini
	-v /data/evm-gateway:/data \
```

The `ExecStart` should look like:
```ini
ExecStart=docker run --rm \
	--name flow-evm-gateway \
	-v /data/evm-gateway:/data \
	us-west1-docker.pkg.dev/dl-flow-devex-production/development/flow-evm-gateway:${VERSION} \
    --database-dir=/data \
```

### 3. Reload systemd and Restart

```bash
sudo systemctl daemon-reload
sudo systemctl restart flow-evm-gateway
```

### 4. Verify

```bash
# Check database directory exists and has files
ls -lah /data/evm-gateway/

# Check logs to see if it's using persisted database
sudo journalctl -u flow-evm-gateway --since "2 minutes ago" | grep -E "start-cadence-height|latest-cadence-height"
```

## Important Notes

1. **First restart**: The gateway will still start from the beginning (the previous database was lost), but future restarts will preserve state
2. **No code changes**: This is purely a Docker volume mount configuration change
3. **Same Docker image**: You're using the same image, just mounting a volume now

## Why No Rebuild?

The fix is in the **Docker run command** (volume mount), not in the gateway code. The gateway code already:
- ✅ Uses `--database-dir=/data` flag
- ✅ Stores database in PebbleDB
- ✅ Reads from database on startup

The only missing piece was **persisting `/data` from container to host**, which is a Docker configuration issue, not a code issue.

