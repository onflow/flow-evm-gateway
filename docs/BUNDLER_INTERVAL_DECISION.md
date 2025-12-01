# Bundler Interval Configuration Decision

## Decision

The bundler interval is set to **800ms (0.8 seconds)** by default, configurable via the `--bundler-interval` flag.

## Background

The bundler periodically checks the UserOperation pool for pending operations and creates `EntryPoint.handleOps()` transactions. The interval determines how frequently this check occurs.

## Impact Analysis

### Performance Characteristics

**When there are NO pending UserOperations:**
- **Cost per run**: ~10-100 microseconds (in-memory map iteration)
- **Impact**: Negligible CPU usage
- **At 0.8s interval**: ~125μs/second CPU time

**When there ARE pending UserOperations:**
- **Cost per batch**: ~60-250ms (RPC calls to Access Node)
  - `GetLatestEVMHeight()`: 10-50ms
  - `EstimateGas()`: 50-200ms (simulates execution)
- **Impact**: Significant RPC load on Access Node
- **At 0.8s interval**: ~75-312ms/second of RPC calls per active batch

### Comparison: 0.8s vs 5s Interval

| Scenario | 5s Interval | 0.8s Interval | Impact |
|----------|-------------|---------------|--------|
| **No UserOps** | ~50μs every 5s | ~50μs every 0.8s | Negligible (6.25x more but still microseconds) |
| **1 UserOp every 5min** | 2 RPC calls every 5s (when UserOp exists) | 2 RPC calls every 0.8s (when UserOp exists) | **6.25x more RPC calls** |
| **Steady stream** | 2 RPC calls per batch every 5s | 2 RPC calls per batch every 0.8s | **6.25x more RPC load on Access Node** |

### Latency Impact

**Average wait time for UserOperation processing:**
- **5s interval**: 0-5 seconds (average: 2.5 seconds)
- **0.8s interval**: 0-0.8 seconds (average: 0.4 seconds)
- **Improvement**: ~2.1 seconds faster average processing time

## Considerations

### Advantages of 0.8s Interval

1. **Lower Latency**: UserOperations are processed faster, improving user experience
2. **Better Responsiveness**: Faster feedback for applications using ERC-4337
3. **Competitive**: Matches or exceeds performance of other bundler implementations

### Disadvantages and Risks

1. **Increased RPC Load**: 6.25x more RPC calls to Access Node when UserOps are present
2. **Access Node Stress**: Higher frequency may stress Access Node under high traffic
3. **Rate Limiting Risk**: Access Node may rate limit if too many requests are made
4. **Resource Usage**: Slightly higher CPU usage (negligible when no UserOps)

### When to Adjust

**Consider increasing interval (e.g., 5s) if:**
- Access Node shows signs of stress or rate limiting
- High traffic volume (> 50 UserOps/minute)
- Network latency to Access Node is high
- Cost optimization is more important than latency

**Consider decreasing interval (e.g., 400ms) if:**
- Very low traffic (< 10 UserOps/minute)
- Latency is critical (real-time applications)
- Access Node can handle the load
- Monitoring shows no issues

## Configuration

The bundler interval can be configured via command-line flag:

```bash
--bundler-interval=800ms  # Default: 800ms (0.8 seconds)
--bundler-interval=5s     # Conservative: 5 seconds
--bundler-interval=400ms  # Aggressive: 400ms (0.4 seconds)
```

## Monitoring

Monitor the following metrics to assess the impact:

1. **Access Node RPC Latency**: Watch for increased latency
2. **Access Node Error Rate**: Monitor for rate limiting or errors
3. **Bundler Processing Time**: Track how long bundling takes
4. **UserOperation Processing Latency**: Measure end-to-end processing time

### Key Metrics

- `evm_gateway_api_errors_total`: Total API errors (watch for Access Node errors)
- `evm_gateway_api_request_duration_seconds`: API request durations
- Custom bundler metrics (if added): Bundler processing time, RPC call counts

## Recommendations

### For Low Traffic (< 10 UserOps/minute)
- **Recommended**: 0.8s (default) or lower (400ms)
- **Rationale**: Minimal RPC load, latency benefits are valuable

### For Medium Traffic (10-50 UserOps/minute)
- **Recommended**: 0.8s (default)
- **Rationale**: Balanced approach, monitor Access Node load

### For High Traffic (> 50 UserOps/minute)
- **Recommended**: 2-5s
- **Rationale**: Reduce Access Node load, batch more UserOps per run

### For Production
- **Recommended**: Start with 0.8s, monitor, adjust based on metrics
- **Rationale**: Default provides good balance, adjust based on actual load

## Future Considerations

1. **Adaptive Interval**: Consider making the interval adaptive based on:
   - Number of pending UserOps
   - Access Node latency
   - Error rates

2. **Smart Batching**: Only run bundler when UserOps are present (event-driven)

3. **Caching**: Cache `GetLatestEVMHeight()` results to reduce RPC calls

4. **Rate Limiting**: Implement backoff if Access Node rate limits

## References

- [ERC-4337 Specification](https://eips.ethereum.org/EIPS/eip-4337)
- [Flow EVM Gateway Setup](https://developers.flow.com/protocol/node-ops/evm-gateway/evm-gateway-setup)
- [Deployment and Testing Guide](./DEPLOYMENT_AND_TESTING.md)

