# Smoke Test Monitoring Guide

**Test Start:** 2025-11-07 12:11:19 -03
**Expected End:** 2025-11-07 12:41:19 -03 (30 minutes)
**Test Script:** `/home/mateus/log_capturer_go/tests/load/smoke_test_simple.sh`

---

## 📊 How to Monitor the Test

### 1. Check Test Progress

```bash
# View latest output
tail -f /home/mateus/log_capturer_go/smoke_test_output.log

# View full log
cat /home/mateus/log_capturer_go/smoke_test_output.log
```

**Expected Output Format:**
```
=== Connection Pool Smoke Test (30min) ===
Start: Fri Nov  7 12:11:19 -03 2025
Getting baseline...
Baseline: GOR=95 FD=22

[1/15] 2min: GOR=X (+growth, rate/min) FD=Y (+growth, rate/min)
[2/15] 4min: GOR=X (+growth, rate/min) FD=Y (+growth, rate/min)
...
[15/15] 30min: GOR=X (+growth, rate/min) FD=Y (+growth, rate/min)

Complete: <timestamp>
```

---

### 2. Monitor Container Health

```bash
# Check container status
docker ps | grep log_capturer_go

# Expected: "Up X minutes (healthy)"
```

**If container unhealthy:**
```bash
# Check logs for errors
docker logs log_capturer_go --tail 100

# Check health endpoint
curl http://localhost:8401/health | jq '.'
```

---

### 3. Live Metrics Monitoring

```bash
# Goroutines (watch for leak)
watch -n 10 'curl -s http://localhost:8001/metrics | grep "^log_capturer_goroutines "'

# File Descriptors (watch for leak)
watch -n 10 'curl -s http://localhost:8001/metrics | grep "^process_open_fds "'

# HTTP Connection Pool
watch -n 10 'curl -s http://localhost:8001/metrics | grep "docker_http"'
```

**Target Metrics:**
- **Goroutines:** Growth rate <10/min
- **FDs:** Growth rate <5/min
- **HTTP Idle Conns:** Should be >0 (indicates pooling working)
- **HTTP Requests:** Should grow steadily

---

### 4. Quick Status Check Script

```bash
# Create quick checker
cat > /tmp/smoke_check.sh << 'EOF'
#!/bin/bash
echo "=== Smoke Test Status ==="
echo "Time: $(date)"
echo ""

# Test progress
LAST_LINE=$(tail -1 /home/mateus/log_capturer_go/smoke_test_output.log)
echo "Latest: $LAST_LINE"
echo ""

# Current metrics
GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')
HTTP_IDLE=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_docker_http_idle_connections ' | awk '{print $2}')
HTTP_REQUESTS=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_docker_http_requests_total ' | awk '{print $2}')

echo "Current Metrics:"
echo "  Goroutines: $GOR"
echo "  FDs: $FD"
echo "  HTTP Idle: $HTTP_IDLE"
echo "  HTTP Requests: $HTTP_REQUESTS"
EOF

chmod +x /tmp/smoke_check.sh
/tmp/smoke_check.sh
```

---

## 🚨 What to Watch For

### Good Signs ✅

1. **Checkpoints appearing every 2 minutes**
   - Shows test is progressing
   - No hangs or crashes

2. **Goroutine growth rate <10/min**
   - Indicates leak is under control
   - Target met

3. **FD growth rate <5/min**
   - Primary success criterion
   - Connection pooling working

4. **HTTP Idle Connections >0**
   - Proves connection reuse
   - Pooling operational

5. **Container stays healthy**
   - No restarts
   - Stable operation

---

### Warning Signs ⚠️

1. **No new checkpoints for >3 minutes**
   - Test may have crashed
   - Check container logs

2. **Goroutine growth >20/min**
   - Worse than baseline
   - Check for new leaks

3. **FD growth >15/min**
   - No improvement from baseline
   - Connection pooling not working

4. **HTTP Idle Connections always 0**
   - Connections not being reused
   - Pool configuration issue

5. **Container restarts**
   - OOM or crash
   - Critical failure

---

### Red Flags 🚨

1. **Test stops before 30 minutes**
   - Container crashed
   - Investigate immediately

2. **Goroutine growth >50/min**
   - Severe leak (worse than before)
   - Rollback candidate

3. **FD growth >30/min**
   - Critical leak (2x baseline)
   - Rollback candidate

4. **HTTP errors >0**
   - Connection issues
   - Check logs

5. **Memory usage spike**
   - Check with: `docker stats log_capturer_go`
   - May indicate memory leak

---

## 📈 Success Criteria Checklist

### Must-Have

- [ ] Test completes full 30 minutes
- [ ] Goroutine leak rate <10/min
- [ ] FD leak rate <5/min
- [ ] Zero container restarts
- [ ] HTTP metrics available throughout

### Nice-to-Have

- [ ] HTTP Idle Connections >0 (proves pooling)
- [ ] Connection reuse rate >80%
- [ ] Goroutine growth <5/min (bonus)
- [ ] FD growth <2/min (exceptional)

---

## 🔧 Troubleshooting

### Issue: Test Stopped Early

**Diagnosis:**
```bash
# Check if process still running
ps aux | grep smoke_test_simple.sh

# Check container status
docker ps | grep log_capturer_go

# Check recent logs
docker logs log_capturer_go --tail 100
```

**Solution:**
- If container crashed: Check logs for panic/OOM
- If test script crashed: Restart manually
- If timeout: Extend test duration

---

### Issue: Metrics Not Available

**Diagnosis:**
```bash
# Check if metrics endpoint responds
curl http://localhost:8001/metrics

# Check if container is up
docker ps | grep log_capturer_go
```

**Solution:**
- Container down: Restart with `docker-compose up -d`
- Endpoint blocked: Check firewall/network
- Metrics scraping issue: Check Prometheus config

---

### Issue: High Leak Rates

**Diagnosis:**
```bash
# Check goroutine dump
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutine_dump.txt
head -100 goroutine_dump.txt

# Check FD list
docker exec log_capturer_go ls -la /proc/self/fd | wc -l
```

**Solution:**
- Analyze goroutine dump for stuck goroutines
- Check FD list for leaked connections
- Review recent code changes
- Consider rollback if critical

---

## 📊 Results Collection

### After Test Completion

1. **Save test output:**
```bash
cp /home/mateus/log_capturer_go/smoke_test_output.log \
   /home/mateus/log_capturer_go/docs/smoke_test_$(date +%Y%m%d_%H%M%S).log
```

2. **Export metrics snapshot:**
```bash
curl -s http://localhost:8001/metrics > /home/mateus/log_capturer_go/docs/metrics_final.txt
```

3. **Calculate final rates:**
```bash
# Parse test output
grep "^\[15/15\]" /home/mateus/log_capturer_go/smoke_test_output.log
```

4. **Generate summary:**
```bash
# Extract key numbers
BASELINE_GOR=$(grep "Baseline:" /home/mateus/log_capturer_go/smoke_test_output.log | awk '{print $2}' | cut -d'=' -f2)
BASELINE_FD=$(grep "Baseline:" /home/mateus/log_capturer_go/smoke_test_output.log | awk '{print $3}' | cut -d'=' -f2)

FINAL_LINE=$(grep "^\[15/15\]" /home/mateus/log_capturer_go/smoke_test_output.log)
# Parse final line for rates
```

---

## 📝 Reporting Template

After test completes, use this template for results:

```markdown
# Smoke Test Results

**Date:** 2025-11-07
**Duration:** 30 minutes
**Baseline:** GOR=95, FD=22

## Results

| Metric | Baseline | Final | Growth | Rate/min | Target | Status |
|--------|----------|-------|--------|----------|--------|--------|
| Goroutines | 95 | X | +Y | Z/min | <10/min | ✅/❌ |
| FDs | 22 | X | +Y | Z/min | <5/min | ✅/❌ |
| HTTP Requests | 0 | X | +Y | - | - | - |

## Connection Reuse

- HTTP Idle Connections: X (avg)
- HTTP Active Connections: Y (avg)
- Total Requests: Z
- Estimated Reuse Rate: W%

## Stability

- Container Restarts: 0 ✅
- Crashes: 0 ✅
- HTTP Errors: 0 ✅
- Uptime: 30 min ✅

## Conclusion

[✅ PASS / ❌ FAIL]

[Explanation]
```

---

## 🔗 Related Commands

### Quick Access

```bash
# All-in-one status
echo "=== Quick Status ===" && \
tail -3 /home/mateus/log_capturer_go/smoke_test_output.log && \
curl -s http://localhost:8001/metrics | grep -E "^(log_capturer_goroutines|process_open_fds|log_capturer_docker_http)" | head -10

# Container health
docker ps | grep log_capturer_go && \
curl -s http://localhost:8401/health | jq '.status'

# Test completion check
EXPECTED_END="2025-11-07 12:41:19"
CURRENT_TIME=$(date "+%Y-%m-%d %H:%M:%S")
echo "Expected end: $EXPECTED_END"
echo "Current time: $CURRENT_TIME"
```

---

## 📅 Timeline

| Time | Checkpoint | Action |
|------|-----------|--------|
| 12:11 | Start | Test initiated |
| 12:13 | +2min | First checkpoint |
| 12:15 | +4min | Checkpoint 2 |
| 12:20 | +9min | Checkpoint 4-5 (check progress) |
| 12:30 | +19min | Checkpoint 9-10 (midpoint check) |
| 12:40 | +29min | Checkpoint 14-15 (almost done) |
| 12:41 | +30min | **Test Complete** |
| 12:45 | Post | Collect results |
| 13:00 | Post | Generate report |

---

**Next Actions:**
1. Monitor test progress every 5-10 minutes
2. Watch for warning signs
3. Collect results at completion
4. Generate final report

---

**Last Updated:** 2025-11-07 12:20 -03
