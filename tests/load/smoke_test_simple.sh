#!/bin/bash
# Simple smoke test - 30 minutes

echo "=== Connection Pool Smoke Test (30min) ==="
echo "Start: $(date)"
sleep 30
echo "Getting baseline..."

BASELINE_GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
BASELINE_FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')

echo "Baseline: GOR=$BASELINE_GOR FD=$BASELINE_FD"

for i in {1..15}; do
    sleep 120
    CURRENT_GOR=$(curl -s http://localhost:8001/metrics | grep '^log_capturer_goroutines ' | awk '{print $2}')
    CURRENT_FD=$(curl -s http://localhost:8001/metrics | grep '^process_open_fds ' | awk '{print $2}')
    ELAPSED=$((i * 2))
    
    GOR_GROWTH=$((CURRENT_GOR - BASELINE_GOR))
    FD_GROWTH=$((CURRENT_FD - BASELINE_FD))
    GOR_RATE=$(echo "scale=2; $GOR_GROWTH / $ELAPSED" | bc)
    FD_RATE=$(echo "scale=2; $FD_GROWTH / $ELAPSED" | bc)
    
    echo "[$i/15] ${ELAPSED}min: GOR=$CURRENT_GOR (+$GOR_GROWTH, ${GOR_RATE}/min) FD=$CURRENT_FD (+$FD_GROWTH, ${FD_RATE}/min)"
done

echo "Complete: $(date)"
