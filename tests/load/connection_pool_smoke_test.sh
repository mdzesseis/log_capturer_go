#!/bin/bash
# Connection Pool Smoke Test (30 minutes)
# FASE 4: Validates FD leak reduction with HTTP connection pooling

echo "======================================================================="
echo "CONNECTION POOL SMOKE TEST - 30 MINUTES"
echo "======================================================================="
echo "Target: Validate FD leak reduction (<5 FD/min)"
echo "Start: $(date '+%Y-%m-%d %H:%M:%S')"
echo ""

# Wait for stabilization
echo "Waiting 30s for services to stabilize..."
sleep 30

# Get baseline
BASELINE_GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
BASELINE_FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')
BASELINE_HTTP_IDLE=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_docker_http_idle_connections ' | awk '{print $2}' || echo "0")
BASELINE_HTTP_ACTIVE=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_docker_http_active_connections ' | awk '{print $2}' || echo "0")
BASELINE_HTTP_REQUESTS=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_docker_http_requests_total ' | awk '{print $2}' || echo "0")

echo "Baseline Metrics:"
echo "  Goroutines: $BASELINE_GOR"
echo "  File Descriptors: $BASELINE_FD"
echo "  HTTP Idle Connections: $BASELINE_HTTP_IDLE"
echo "  HTTP Active Connections: $BASELINE_HTTP_ACTIVE"
echo "  HTTP Total Requests: $BASELINE_HTTP_REQUESTS"
echo ""

TEST_DURATION=$((30 * 60))  # 30 minutes
CHECKPOINTS=15
INTERVAL=$((TEST_DURATION / CHECKPOINTS))
START_TIME=$(date +%s)

LOG_FILE="/home/mateus/log_capturer_go/connection_pool_smoke_test.log"
> "$LOG_FILE"

for i in $(seq 1 $CHECKPOINTS); do
    ELAPSED=$(($(date +%s) - START_TIME))

    echo "======================================================================" | tee -a "$LOG_FILE"
    echo "CHECKPOINT $i/$CHECKPOINTS - Elapsed: $((ELAPSED / 60))min (${ELAPSED}s)" | tee -a "$LOG_FILE"
    echo "Time: $(date '+%Y-%m-%d %H:%M:%S')" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"

    # Get current metrics
    CURRENT_GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
    CURRENT_FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')
    CURRENT_HTTP_IDLE=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_docker_http_idle_connections ' | awk '{print $2}' || echo "0")
    CURRENT_HTTP_ACTIVE=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_docker_http_active_connections ' | awk '{print $2}' || echo "0")
    CURRENT_HTTP_REQUESTS=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_docker_http_requests_total ' | awk '{print $2}' || echo "0")

    GOR_GROWTH=$((CURRENT_GOR - BASELINE_GOR))
    FD_GROWTH=$((CURRENT_FD - BASELINE_FD))
    HTTP_REQUESTS_GROWTH=$((CURRENT_HTTP_REQUESTS - BASELINE_HTTP_REQUESTS))

    # Calculate rates
    ELAPSED_MINUTES=$(echo "scale=2; $ELAPSED / 60" | bc)
    GOR_RATE=$(echo "scale=2; $GOR_GROWTH / $ELAPSED_MINUTES" | bc)
    FD_RATE=$(echo "scale=2; $FD_GROWTH / $ELAPSED_MINUTES" | bc)

    # Display goroutines
    echo "Goroutines: $CURRENT_GOR (growth: $GOR_GROWTH, rate: ${GOR_RATE}/min)" | tee -a "$LOG_FILE"
    if (( $(echo "$GOR_RATE < 10" | bc -l) )); then
        echo "  ✅ TARGET MET (<10/min)" | tee -a "$LOG_FILE"
    else
        echo "  ⚠️ ABOVE TARGET (>10/min)" | tee -a "$LOG_FILE"
    fi
    echo "" | tee -a "$LOG_FILE"

    # Display FDs
    echo "File Descriptors: $CURRENT_FD (growth: $FD_GROWTH, rate: ${FD_RATE}/min)" | tee -a "$LOG_FILE"
    if (( $(echo "$FD_RATE < 5" | bc -l) )); then
        echo "  ✅ TARGET MET (<5/min)" | tee -a "$LOG_FILE"
    else
        echo "  ⚠️ ABOVE TARGET (>5/min)" | tee -a "$LOG_FILE"
    fi
    echo "" | tee -a "$LOG_FILE"

    # Display HTTP connection pool metrics
    echo "HTTP Connection Pool Metrics:" | tee -a "$LOG_FILE"
    echo "  Idle Connections: $CURRENT_HTTP_IDLE" | tee -a "$LOG_FILE"
    echo "  Active Connections: $CURRENT_HTTP_ACTIVE" | tee -a "$LOG_FILE"
    echo "  Total Requests: $CURRENT_HTTP_REQUESTS (growth: $HTTP_REQUESTS_GROWTH)" | tee -a "$LOG_FILE"

    # Calculate connection reuse rate (if possible)
    if [ "$HTTP_REQUESTS_GROWTH" -gt 0 ] && [ "$FD_GROWTH" -ge 0 ]; then
        REUSE_RATIO=$(echo "scale=2; 100 * (1 - ($FD_GROWTH / $HTTP_REQUESTS_GROWTH))" | bc)
        echo "  Connection Reuse Rate: ~${REUSE_RATIO}%" | tee -a "$LOG_FILE"
        if (( $(echo "$REUSE_RATIO > 80" | bc -l) )); then
            echo "  ✅ Excellent reuse (>80%)" | tee -a "$LOG_FILE"
        elif (( $(echo "$REUSE_RATIO > 50" | bc -l) )); then
            echo "  ⚠️ Moderate reuse (50-80%)" | tee -a "$LOG_FILE"
        else
            echo "  ❌ Poor reuse (<50%)" | tee -a "$LOG_FILE"
        fi
    fi
    echo "" | tee -a "$LOG_FILE"

    # Display container stream metrics
    ACTIVE_STREAMS=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_active_streams ' | awk '{print $2}')
    echo "Container Streams: $ACTIVE_STREAMS active" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"

    echo "======================================================================" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"

    if [ $i -lt $CHECKPOINTS ]; then
        sleep $INTERVAL
    fi
done

# Final results
echo "======================================================================" | tee -a "$LOG_FILE"
echo "SMOKE TEST COMPLETE" | tee -a "$LOG_FILE"
echo "======================================================================" | tee -a "$LOG_FILE"

FINAL_GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
FINAL_FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')
FINAL_HTTP_REQUESTS=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_docker_http_requests_total ' | awk '{print $2}' || echo "0")

TOTAL_GOR_GROWTH=$((FINAL_GOR - BASELINE_GOR))
TOTAL_FD_GROWTH=$((FINAL_FD - BASELINE_FD))
TOTAL_HTTP_REQUESTS_GROWTH=$((FINAL_HTTP_REQUESTS - BASELINE_HTTP_REQUESTS))

FINAL_GOR_RATE=$(echo "scale=2; $TOTAL_GOR_GROWTH / 30" | bc)
FINAL_FD_RATE=$(echo "scale=2; $TOTAL_FD_GROWTH / 30" | bc)

echo "" | tee -a "$LOG_FILE"
echo "Final Results (30 minutes):" | tee -a "$LOG_FILE"
echo "  Goroutine Growth: $TOTAL_GOR_GROWTH (${FINAL_GOR_RATE}/min)" | tee -a "$LOG_FILE"
echo "  FD Growth: $TOTAL_FD_GROWTH (${FINAL_FD_RATE}/min)" | tee -a "$LOG_FILE"
echo "  HTTP Requests: $TOTAL_HTTP_REQUESTS_GROWTH" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"

# Calculate final reuse rate
if [ "$TOTAL_HTTP_REQUESTS_GROWTH" -gt 0 ] && [ "$TOTAL_FD_GROWTH" -ge 0 ]; then
    FINAL_REUSE_RATIO=$(echo "scale=2; 100 * (1 - ($TOTAL_FD_GROWTH / $TOTAL_HTTP_REQUESTS_GROWTH))" | bc)
    echo "  Connection Reuse Rate: ${FINAL_REUSE_RATIO}%" | tee -a "$LOG_FILE"
fi
echo "" | tee -a "$LOG_FILE"

# Success criteria
SUCCESS=true

if (( $(echo "$FINAL_GOR_RATE < 10" | bc -l) )); then
    echo "✅ SUCCESS: Goroutine leak target met (<10/min)" | tee -a "$LOG_FILE"
else
    echo "❌ FAILED: Goroutine leak above target (${FINAL_GOR_RATE}/min > 10/min)" | tee -a "$LOG_FILE"
    SUCCESS=false
fi

if (( $(echo "$FINAL_FD_RATE < 5" | bc -l) )); then
    echo "✅ SUCCESS: FD leak target met (<5/min)" | tee -a "$LOG_FILE"
else
    echo "❌ FAILED: FD leak above target (${FINAL_FD_RATE}/min > 5/min)" | tee -a "$LOG_FILE"
    SUCCESS=false
fi

echo "" | tee -a "$LOG_FILE"
echo "Log saved to: $LOG_FILE" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"

# Comparison with baseline (FASE 6H.1)
echo "======================================================================" | tee -a "$LOG_FILE"
echo "COMPARISON WITH BASELINE (FASE 6H.1)" | tee -a "$LOG_FILE"
echo "======================================================================" | tee -a "$LOG_FILE"
echo "Baseline (Hybrid Short-Lived):" | tee -a "$LOG_FILE"
echo "  Goroutine leak: 31.4/min" | tee -a "$LOG_FILE"
echo "  FD leak: 15.7/min" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"
echo "Current (Connection Pooling):" | tee -a "$LOG_FILE"
echo "  Goroutine leak: ${FINAL_GOR_RATE}/min" | tee -a "$LOG_FILE"
echo "  FD leak: ${FINAL_FD_RATE}/min" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"

# Calculate improvements
GOR_IMPROVEMENT=$(echo "scale=2; 100 * (1 - ($FINAL_GOR_RATE / 31.4))" | bc)
FD_IMPROVEMENT=$(echo "scale=2; 100 * (1 - ($FINAL_FD_RATE / 15.7))" | bc)

echo "Improvement:" | tee -a "$LOG_FILE"
echo "  Goroutine leak: ${GOR_IMPROVEMENT}% reduction" | tee -a "$LOG_FILE"
echo "  FD leak: ${FD_IMPROVEMENT}% reduction" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"

if $SUCCESS; then
    echo "🎉 SMOKE TEST PASSED!" | tee -a "$LOG_FILE"
    exit 0
else
    echo "⚠️ SMOKE TEST FAILED - Review results above" | tee -a "$LOG_FILE"
    exit 1
fi
