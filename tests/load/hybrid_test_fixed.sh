#!/bin/bash
# Hybrid Monitor Smoke Test (30 minutes)

echo "======================================================================="
echo "HYBRID MONITOR SMOKE TEST - 30 MINUTES"
echo "Start: $(date '+%Y-%m-%d %H:%M:%S')"
echo ""

# Wait for stabilization
echo "Waiting 30s for stabilization..."
sleep 30

# Get baseline
BASELINE_GOR=$(curl -s http://localhost:8002/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
BASELINE_FD=$(curl -s http://localhost:8002/metrics | grep '^process_open_fds ' | awk '{print $2}')

echo "=== HYBRID MONITOR BASELINE ==="
echo "  Goroutines: $BASELINE_GOR"
echo "  FDs: $BASELINE_FD"

# Get Connection Pool for comparison
CONNPOOL_GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
CONNPOOL_FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')

echo "=== CONNECTION POOL (comparison) ==="
echo "  Goroutines: $CONNPOOL_GOR"
echo "  FDs: $CONNPOOL_FD"
echo ""

LOG_FILE="/home/mateus/log_capturer_go/hybrid_monitor_smoke_test.log"
> "$LOG_FILE"

echo "Starting 30-minute test..." | tee -a "$LOG_FILE"

# 15 checkpoints, 2 minutes each
for i in {1..15}; do
    ELAPSED=$((i * 2))
    
    echo "======================================================================" | tee -a "$LOG_FILE"
    echo "CHECKPOINT $i/15 - Elapsed: ${ELAPSED}min" | tee -a "$LOG_FILE"
    echo "Time: $(date '+%Y-%m-%d %H:%M:%S')" | tee -a "$LOG_FILE"
    
    # Get current metrics
    CURRENT_GOR=$(curl -s http://localhost:8002/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
    CURRENT_FD=$(curl -s http://localhost:8002/metrics | grep '^process_open_fds ' | awk '{print $2}')
    
    GOR_GROWTH=$((CURRENT_GOR - BASELINE_GOR))
    FD_GROWTH=$((CURRENT_FD - BASELINE_FD))
    
    # Calculate rates
    GOR_RATE=$(echo "scale=2; $GOR_GROWTH / $ELAPSED" | bc)
    FD_RATE=$(echo "scale=2; $FD_GROWTH / $ELAPSED" | bc)
    
    echo "Goroutines: $CURRENT_GOR (growth: $GOR_GROWTH, rate: ${GOR_RATE}/min)" | tee -a "$LOG_FILE"
    echo "FDs: $CURRENT_FD (growth: $FD_GROWTH, rate: ${FD_RATE}/min)" | tee -a "$LOG_FILE"
    
    # Connection Pool comparison
    CONNPOOL_GOR=$(curl -s http://localhost:8001/metrics 2>/dev/null | grep '^log_capturer_goroutines ' | awk '{print $2}' || echo "N/A")
    echo "Connection Pool: Gor=$CONNPOOL_GOR" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"
    
    if [ $i -lt 15 ]; then
        sleep 120
    fi
done

# Final results
FINAL_GOR=$(curl -s http://localhost:8002/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
FINAL_FD=$(curl -s http://localhost:8002/metrics | grep '^process_open_fds ' | awk '{print $2}')

TOTAL_GOR_GROWTH=$((FINAL_GOR - BASELINE_GOR))
TOTAL_FD_GROWTH=$((FINAL_FD - BASELINE_FD))

FINAL_GOR_RATE=$(echo "scale=2; $TOTAL_GOR_GROWTH / 30" | bc)
FINAL_FD_RATE=$(echo "scale=2; $TOTAL_FD_GROWTH / 30" | bc)

echo "======================================================================" | tee -a "$LOG_FILE"
echo "FINAL RESULTS" | tee -a "$LOG_FILE"
echo "  Goroutine Growth: $TOTAL_GOR_GROWTH (${FINAL_GOR_RATE}/min)" | tee -a "$LOG_FILE"
echo "  FD Growth: $TOTAL_FD_GROWTH (${FINAL_FD_RATE}/min)" | tee -a "$LOG_FILE"
echo ""

if (( $(echo "$FINAL_GOR_RATE < 2" | bc -l) )); then
    echo "SUCCESS: ZERO GOROUTINE LEAK!" | tee -a "$LOG_FILE"
else
    echo "WARNING: Goroutine leak detected" | tee -a "$LOG_FILE"
fi

echo "Log file: $LOG_FILE"
