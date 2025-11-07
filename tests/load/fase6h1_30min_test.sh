#!/bin/bash

# FASE 6H.1 - 30-Minute Validation Test
# Tests heartbeat fix to prevent 6-minute task timeouts
# Expected: No timeouts, goroutine leak ~30-35 gor/min (similar to FASE 6G)

set -e

echo "======================================================================"
echo "FASE 6H.1 - 30-MINUTE VALIDATION TEST (HEARTBEAT FIX)"
echo "======================================================================"
echo "Target: 8 containers, 30 minutes (15 checkpoints)"
echo "Expected: No 6-minute timeouts, stable monitoring"
echo "Expected leak: ~30-35 gor/min (controlled, same as FASE 6G)"
echo "Start: $(date '+%Y-%m-%d %H:%M:%S')"
echo ""
echo "TESTING HEARTBEAT FIX: Prevents Task Manager timeout"
echo ""
echo "Previous issue (FASE 6H):"
echo "  - No heartbeat → Task Manager timeout after 6 minutes"
echo "  - All containers stopped monitoring"
echo ""
echo "Fix applied:"
echo "  - Heartbeat ticker (30s intervals)"
echo "  - Heartbeat after successful rotation"
echo ""
echo "Waiting for services to stabilize (30s)..."
sleep 30

# Get baseline
echo "Getting baseline metrics..."
BASELINE_GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
BASELINE_FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')
BASELINE_STREAMS=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_active_streams ' | awk '{print $2}' || echo "0")
BASELINE_ROTATIONS=$(curl -s http://localhost:8001/metrics | grep 'log_capturer_stream_rotations_total' | wc -l)

echo "Baseline established:"
echo "  Goroutines: $BASELINE_GOR"
echo "  File Descriptors: $BASELINE_FD"
echo "  Active Streams: $BASELINE_STREAMS"
echo "  Container Rotations: $BASELINE_ROTATIONS"
echo ""

TEST_DURATION=$((30 * 60))  # 30 minutes
CHECKPOINTS=15
INTERVAL=$((TEST_DURATION / CHECKPOINTS))
START_TIME=$(date +%s)

# Output file
LOG_FILE="/home/mateus/log_capturer_go/fase6h1_30min.log"
> "$LOG_FILE"

for i in $(seq 1 $CHECKPOINTS); do
    ELAPSED=$(($(date +%s) - START_TIME))
    CHECKPOINT_TIME=$(date '+%Y-%m-%d %H:%M:%S')

    echo "======================================================================" | tee -a "$LOG_FILE"
    echo "CHECKPOINT $i/$CHECKPOINTS - Elapsed: $((ELAPSED / 60))min (${ELAPSED}s)" | tee -a "$LOG_FILE"
    echo "======================================================================" | tee -a "$LOG_FILE"
    echo "Time: $CHECKPOINT_TIME" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"

    # Get current metrics
    CURRENT_GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
    CURRENT_FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')
    CURRENT_STREAMS=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_active_streams ' | awk '{print $2}' || echo "0")
    CURRENT_ROTATIONS=$(curl -s http://localhost:8001/metrics | grep 'log_capturer_stream_rotations_total' | wc -l)

    GOR_GROWTH=$((CURRENT_GOR - BASELINE_GOR))
    FD_GROWTH=$((CURRENT_FD - BASELINE_FD))
    ROTATION_GROWTH=$((CURRENT_ROTATIONS - BASELINE_ROTATIONS))

    # Calculate growth rate
    GOR_RATE=$(echo "scale=2; $GOR_GROWTH / ($ELAPSED / 60)" | bc)

    # Goroutines
    echo "Goroutines: $CURRENT_GOR" | tee -a "$LOG_FILE"
    echo "  Growth: $GOR_GROWTH (${GOR_RATE}/min)" | tee -a "$LOG_FILE"
    if (( $(echo "$GOR_RATE < 2" | bc -l) )); then
        echo "  ✅ EXCELLENT (<2/min)" | tee -a "$LOG_FILE"
    elif (( $(echo "$GOR_RATE < 40" | bc -l) )); then
        echo "  ✓ ACCEPTABLE (<40/min - similar to FASE 6G)" | tee -a "$LOG_FILE"
    else
        echo "  ⚠️ HIGH (>40/min)" | tee -a "$LOG_FILE"
    fi
    echo "" | tee -a "$LOG_FILE"

    # Active Streams
    echo "Active Streams: $CURRENT_STREAMS" | tee -a "$LOG_FILE"
    if [ "$CURRENT_STREAMS" = "0" ]; then
        echo "  ⚠️ WARNING: No active streams detected!" | tee -a "$LOG_FILE"
    else
        echo "  ✅ Monitoring active" | tee -a "$LOG_FILE"
    fi
    echo "" | tee -a "$LOG_FILE"

    # File Descriptors
    echo "File Descriptors: $CURRENT_FD" | tee -a "$LOG_FILE"
    echo "  Growth: $FD_GROWTH" | tee -a "$LOG_FILE"
    if [ $FD_GROWTH -lt 100 ]; then
        echo "  ✅ FD OK (<100)" | tee -a "$LOG_FILE"
    else
        echo "  ❌ FD LEAK (>100)" | tee -a "$LOG_FILE"
    fi
    echo "" | tee -a "$LOG_FILE"

    # Rotations
    echo "Total Rotations: $CURRENT_ROTATIONS" | tee -a "$LOG_FILE"
    echo "  New rotations: $ROTATION_GROWTH" | tee -a "$LOG_FILE"
    if [ $CURRENT_ROTATIONS -gt 0 ]; then
        echo "  ✅ Rotations working!" | tee -a "$LOG_FILE"
    else
        echo "  ⚠️ WARNING: No rotations yet!" | tee -a "$LOG_FILE"
    fi
    echo "" | tee -a "$LOG_FILE"

    # Progress bar
    PERCENT=$((i * 100 / CHECKPOINTS))
    FILLED=$((PERCENT / 5))
    EMPTY=$((20 - FILLED))
    BAR=$(printf "%${FILLED}s" | tr ' ' '█')$(printf "%${EMPTY}s" | tr ' ' '░')
    echo "Progress: [$(printf "%3d" $PERCENT)%] $BAR" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"

    # Check for task timeouts (critical check)
    TASK_TIMEOUTS=$(docker logs log_capturer_go 2>&1 | grep -c "Task timeout detected" || echo "0")
    if [ "$TASK_TIMEOUTS" -gt 0 ]; then
        echo "❌ CRITICAL: $TASK_TIMEOUTS task timeout(s) detected!" | tee -a "$LOG_FILE"
        echo "   Heartbeat fix may not be working!" | tee -a "$LOG_FILE"
        echo "" | tee -a "$LOG_FILE"
    fi

    # Special check at 7-8 minute mark (where FASE 6H failed)
    if [ $i -eq 4 ]; then
        echo "🔍 CRITICAL CHECKPOINT: 7-minute mark (FASE 6H failed here)" | tee -a "$LOG_FILE"
        if [ "$TASK_TIMEOUTS" -eq 0 ] && [ "$CURRENT_STREAMS" != "0" ]; then
            echo "   ✅ PASSED: No timeouts, monitoring still active!" | tee -a "$LOG_FILE"
        else
            echo "   ❌ FAILED: Same issue as FASE 6H" | tee -a "$LOG_FILE"
        fi
        echo "" | tee -a "$LOG_FILE"
    fi

    echo "======================================================================" | tee -a "$LOG_FILE"
    echo "" | tee -a "$LOG_FILE"

    # Wait for next checkpoint
    if [ $i -lt $CHECKPOINTS ]; then
        sleep $INTERVAL
    fi
done

# Final results
FINAL_TIME=$(date '+%Y-%m-%d %H:%M:%S')
TOTAL_ELAPSED=$(($(date +%s) - START_TIME))
FINAL_GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
FINAL_FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')
FINAL_ROTATIONS=$(curl -s http://localhost:8001/metrics | grep 'log_capturer_stream_rotations_total' | wc -l)
TASK_TIMEOUTS=$(docker logs log_capturer_go 2>&1 | grep -c "Task timeout detected" || echo "0")

TOTAL_GOR_GROWTH=$((FINAL_GOR - BASELINE_GOR))
FINAL_GOR_RATE=$(echo "scale=2; $TOTAL_GOR_GROWTH / ($TOTAL_ELAPSED / 60)" | bc)

echo "======================================================================" | tee -a "$LOG_FILE"
echo "TEST COMPLETE - FASE 6H.1 FINISHED" | tee -a "$LOG_FILE"
echo "======================================================================" | tee -a "$LOG_FILE"
echo "End time: $FINAL_TIME" | tee -a "$LOG_FILE"
echo "Duration: 30 minutes" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"

echo "📊 FINAL RESULTS:" | tee -a "$LOG_FILE"
echo "================" | tee -a "$LOG_FILE"
echo "Baseline Goroutines: $BASELINE_GOR" | tee -a "$LOG_FILE"
echo "Final Goroutines: $FINAL_GOR" | tee -a "$LOG_FILE"
echo "Total Growth: $TOTAL_GOR_GROWTH" | tee -a "$LOG_FILE"
echo "Average Growth Rate: ${FINAL_GOR_RATE}/min" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"

# Success criteria
if [ "$TASK_TIMEOUTS" -eq 0 ]; then
    echo "✅ SUCCESS: No task timeouts (heartbeat fix working!)" | tee -a "$LOG_FILE"
else
    echo "❌ FAILED: $TASK_TIMEOUTS task timeout(s) detected" | tee -a "$LOG_FILE"
fi
echo "" | tee -a "$LOG_FILE"

if (( $(echo "$FINAL_GOR_RATE < 2" | bc -l) )); then
    echo "✅ EXCELLENT: Growth rate (${FINAL_GOR_RATE}/min) is BELOW target (<2/min)" | tee -a "$LOG_FILE"
    echo "   Goroutine leak eliminated!" | tee -a "$LOG_FILE"
elif (( $(echo "$FINAL_GOR_RATE < 40" | bc -l) )); then
    echo "✓ ACCEPTABLE: Growth rate (${FINAL_GOR_RATE}/min) is within acceptable range" | tee -a "$LOG_FILE"
    echo "   Similar to FASE 6G (~30-35/min), controlled leak" | tee -a "$LOG_FILE"
else
    echo "❌ FAILED: Growth rate (${FINAL_GOR_RATE}/min) EXCEEDS acceptable range" | tee -a "$LOG_FILE"
    echo "   Leak is worse than expected" | tee -a "$LOG_FILE"
fi
echo "" | tee -a "$LOG_FILE"

echo "FD Growth: $((FINAL_FD - BASELINE_FD))" | tee -a "$LOG_FILE"
if [ $((FINAL_FD - BASELINE_FD)) -lt 100 ]; then
    echo "✅ FD stable" | tee -a "$LOG_FILE"
else
    echo "⚠️ FD leak detected" | tee -a "$LOG_FILE"
fi
echo "" | tee -a "$LOG_FILE"

echo "Total Rotations: $FINAL_ROTATIONS" | tee -a "$LOG_FILE"
if [ $FINAL_ROTATIONS -gt 0 ]; then
    echo "✅ Stream rotations working" | tee -a "$LOG_FILE"
else
    echo "❌ No rotations recorded" | tee -a "$LOG_FILE"
fi
echo "" | tee -a "$LOG_FILE"

echo "Log file: $LOG_FILE" | tee -a "$LOG_FILE"
echo "======================================================================" | tee -a "$LOG_FILE"
echo ""
