#!/bin/bash
# Monitor both Connection Pool and Hybrid Monitor tests

while true; do
    clear
    echo "======================================================================="
    echo "PARALLEL SMOKE TESTS MONITORING"
    echo "Time: $(date '+%Y-%m-%d %H:%M:%S')"
    echo "======================================================================="
    echo ""

    # Connection Pool (port 8001)
    echo "=== CONNECTION POOL (feature/connection-pooling-fix) ==="
    CONN_GOR=$(curl -s http://localhost:8001/metrics 2>/dev/null | grep '^log_capturer_goroutines ' | awk '{print $2}' || echo "N/A")
    CONN_FD=$(curl -s http://localhost:8001/metrics 2>/dev/null | grep '^process_open_fds ' | awk '{print $2}' || echo "N/A")
    CONN_HEALTHY=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8401/health 2>/dev/null || echo "N/A")

    echo "  Goroutines: $CONN_GOR"
    echo "  FDs: $CONN_FD"
    echo "  Health: $CONN_HEALTHY"
    echo ""

    # Hybrid Monitor (port 8002)
    echo "=== HYBRID MONITOR (main) ==="
    HYBRID_GOR=$(curl -s http://localhost:8002/metrics 2>/dev/null | grep '^log_capturer_goroutines ' | awk '{print $2}' || echo "N/A")
    HYBRID_FD=$(curl -s http://localhost:8002/metrics 2>/dev/null | grep '^process_open_fds ' | awk '{print $2}' || echo "N/A")
    HYBRID_HEALTHY=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8402/health 2>/dev/null || echo "N/A")

    echo "  Goroutines: $HYBRID_GOR"
    echo "  FDs: $HYBRID_FD"
    echo "  Health: $HYBRID_HEALTHY"

    # Container info
    HYBRID_CONTAINERS=$(docker logs log_capturer_go_hybrid 2>&1 | grep -i "starting container monitoring" | wc -l)
    echo "  Containers monitored: $HYBRID_CONTAINERS"
    echo ""

    # Test progress
    echo "=== TEST PROGRESS ==="
    echo "Connection Pool:"
    tail -3 /home/mateus/log_capturer_go/smoke_test_output.log 2>/dev/null | grep "CHECKPOINT" || echo "  (waiting for checkpoints...)"
    echo ""
    echo "Hybrid Monitor:"
    tail -3 /home/mateus/log_capturer_go/hybrid_monitor_smoke_test.log 2>/dev/null | grep "CHECKPOINT" || echo "  (waiting for checkpoints...)"
    echo ""

    # Container status
    echo "=== CONTAINER STATUS ==="
    docker ps --format "table {{.Names}}\t{{.Status}}" | grep -E "(log_capturer|NAMES)"
    echo ""

    echo "======================================================================="
    echo "Press Ctrl+C to stop monitoring"
    sleep 10
done
