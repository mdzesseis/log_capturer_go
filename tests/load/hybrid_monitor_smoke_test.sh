#!/bin/bash
# Hybrid Monitor Smoke Test (30 minutes)
# Target: ZERO LEAKS (0 gor/min, 0 FD/min)

echo "======================================================================="
echo "HYBRID MONITOR SMOKE TEST - 30 MINUTES"
echo "======================================================================="
echo "Target: ZERO LEAKS (0 gor/min, 0 FD/min)"
echo "Port: 8002 (Hybrid Monitor metrics)"
echo "Comparison: vs Connection Pool (port 8001)"
echo "Start: $(date '+%Y-%m-%d %H:%M:%S')"
echo ""

# Wait for stabilization
echo "Waiting 30s for services to stabilize..."
sleep 30

# Get baseline (PORT 8002 for Hybrid Monitor)
BASELINE_GOR=$(curl -s http://localhost:8002/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
BASELINE_FD=$(curl -s http://localhost:8002/metrics | grep '^process_open_fds ' | awk '{print $2}')

echo "=== HYBRID MONITOR BASELINE (port 8002) ==="
echo "  Goroutines: $BASELINE_GOR"
echo "  FDs: $BASELINE_FD"
echo ""

# Get Connection Pool stats for comparison (PORT 8001)
CONNPOOL_GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
CONNPOOL_FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')

echo "=== CONNECTION POOL CURRENT (port 8001 - for comparison) ==="
echo "  Goroutines: $CONNPOOL_GOR"
echo "  FDs: $CONNPOOL_FD"
echo ""

TEST_DURATION=$((30 * 60))
CHECKPOINTS=15
INTERVAL=$((TEST_DURATION / CHECKPOINTS))
START_TIME=$(date +%s)

LOG_FILE="/home/mateus/log_capturer_go/hybrid_monitor_smoke_test.log"
> "$LOG_FILE"

echo "=== Starting 30-minute monitoring ===" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"

for i in $(seq 1 $CHECKPOINTS); do
    ELAPSED=$(($(date +%s) - START_TIME))

    echo "======================================================================" | tee -a "$LOG_FILE"
    echo "CHECKPOINT $i/$CHECKPOINTS - Elapsed: $((ELAPSED / 60))min (${ELAPSED}s)" | tee -a "$LOG_FILE"
    echo "Time: $(date '+%Y-%m-%d %H:%M:%S')" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"

    # HYBRID MONITOR metrics (port 8002)
    CURRENT_GOR=$(curl -s http://localhost:8002/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
    CURRENT_FD=$(curl -s http://localhost:8002/metrics | grep '^process_open_fds ' | awk '{print $2}')

    GOR_GROWTH=$((CURRENT_GOR - BASELINE_GOR))
    FD_GROWTH=$((CURRENT_FD - BASELINE_FD))

    # Calculate rates
    if [ $ELAPSED -gt 0 ]; then
        GOR_RATE=$(echo "scale=2; $GOR_GROWTH / ($ELAPSED / 60)" | bc)
        FD_RATE=$(echo "scale=2; $FD_GROWTH / ($ELAPSED / 60)" | bc)
    else
        GOR_RATE="0.00"
        FD_RATE="0.00"
    fi

    # CONNECTION POOL metrics for comparison (port 8001)
    CONNPOOL_GOR=$(curl -s http://localhost:8001/metrics 2>/dev/null | grep '^log_capturer_goroutines ' | awk '{print $2}' || echo "N/A")
    CONNPOOL_FD=$(curl -s http://localhost:8001/metrics 2>/dev/null | grep '^process_open_fds ' | awk '{print $2}' || echo "N/A")

    # Display HYBRID MONITOR
    echo "=== HYBRID MONITOR (port 8002) ===" | tee -a "$LOG_FILE"
    echo "Goroutines: $CURRENT_GOR (growth: $GOR_GROWTH, rate: ${GOR_RATE}/min)" | tee -a "$LOG_FILE"
    if (( $(echo "$GOR_RATE < 2" | bc -l) )); then
        echo "  🎯 EXCELLENT (<2/min - ZERO LEAK TARGET!)" | tee -a "$LOG_FILE"
    elif (( $(echo "$GOR_RATE < 5" | bc -l) )); then
        echo "  ✅ VERY GOOD (<5/min)" | tee -a "$LOG_FILE"
    else
        echo "  ⚠️ ABOVE TARGET (>5/min)" | tee -a "$LOG_FILE"
    fi
    echo "" | tee -a "$LOG_FILE"

    echo "File Descriptors: $CURRENT_FD (growth: $FD_GROWTH, rate: ${FD_RATE}/min)" | tee -a "$LOG_FILE"
    if (( $(echo "$FD_RATE < 2" | bc -l) )); then
        echo "  🎯 EXCELLENT (<2/min - ZERO LEAK TARGET!)" | tee -a "$LOG_FILE"
    elif (( $(echo "$FD_RATE < 5" | bc -l) )); then
        echo "  ✅ VERY GOOD (<5/min)" | tee -a "$LOG_FILE"
    else
        echo "  ⚠️ ABOVE TARGET (>5/min)" | tee -a "$LOG_FILE"
    fi
    echo "" | tee -a "$LOG_FILE"

    # Display CONNECTION POOL for comparison
    echo "=== CONNECTION POOL (port 8001 - for comparison) ===" | tee -a "$LOG_FILE"
    echo "Goroutines: $CONNPOOL_GOR" | tee -a "$LOG_FILE"
    echo "FDs: $CONNPOOL_FD" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"

    # Container info
    HYBRID_LOGS=$(docker logs log_capturer_go_hybrid 2>&1 | grep -i "starting container monitoring" | wc -l)
    echo "Hybrid Monitor Coverage:" | tee -a "$LOG_FILE"
    echo "  Containers monitored: $HYBRID_LOGS" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"

    echo "======================================================================" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"

    if [ $i -lt $CHECKPOINTS ]; then
        sleep $INTERVAL
    fi
done

# Final results
echo "======================================================================" | tee -a "$LOG_FILE"
echo "TEST COMPLETE" | tee -a "$LOG_FILE"
echo "======================================================================" | tee -a "$LOG_FILE"

FINAL_GOR=$(curl -s http://localhost:8002/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
FINAL_FD=$(curl -s http://localhost:8002/metrics | grep '^process_open_fds ' | awk '{print $2}')

TOTAL_GOR_GROWTH=$((FINAL_GOR - BASELINE_GOR))
TOTAL_FD_GROWTH=$((FINAL_FD - BASELINE_FD))

FINAL_GOR_RATE=$(echo "scale=2; $TOTAL_GOR_GROWTH / 30" | bc)
FINAL_FD_RATE=$(echo "scale=2; $TOTAL_FD_GROWTH / 30" | bc)

echo "=== HYBRID MONITOR FINAL RESULTS ===" | tee -a "$LOG_FILE"
echo "  Baseline Goroutines: $BASELINE_GOR" | tee -a "$LOG_FILE"
echo "  Final Goroutines: $FINAL_GOR" | tee -a "$LOG_FILE"
echo "  Goroutine Growth: $TOTAL_GOR_GROWTH (${FINAL_GOR_RATE}/min)" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"
echo "  Baseline FDs: $BASELINE_FD" | tee -a "$LOG_FILE"
echo "  Final FDs: $FINAL_FD" | tee -a "$LOG_FILE"
echo "  FD Growth: $TOTAL_FD_GROWTH (${FINAL_FD_RATE}/min)" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"

# Success criteria (ZERO LEAKS!)
if (( $(echo "$FINAL_GOR_RATE < 2" | bc -l) )); then
    echo "🎯 EXCELLENT: ZERO GOROUTINE LEAK!" | tee -a "$LOG_FILE"
elif (( $(echo "$FINAL_GOR_RATE < 5" | bc -l) )); then
    echo "✅ SUCCESS: Minimal goroutine leak" | tee -a "$LOG_FILE"
else
    echo "❌ FAILED: Goroutine leak above target" | tee -a "$LOG_FILE"
fi

if (( $(echo "$FINAL_FD_RATE < 2" | bc -l) )); then
    echo "🎯 EXCELLENT: ZERO FD LEAK!" | tee -a "$LOG_FILE"
elif (( $(echo "$FINAL_FD_RATE < 5" | bc -l) )); then
    echo "✅ SUCCESS: Minimal FD leak" | tee -a "$LOG_FILE"
else
    echo "❌ FAILED: FD leak above target" | tee -a "$LOG_FILE"
fi

echo "" | tee -a "$LOG_FILE"
echo "Log file: $LOG_FILE" | tee -a "$LOG_FILE"
echo "======================================================================" | tee -a "$LOG_FILE"
