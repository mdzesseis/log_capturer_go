#!/bin/bash
echo "=== Monitoring Goroutines for 2 Minutes ==="

for i in 1 2 3 4 5 6 7 8; do
    echo "Sample $i/8:"
    curl -s http://localhost:8401/health | jq '{goroutines: .checks.memory.goroutines, growth_rate: .services.goroutine_tracker.stats.growth_rate_per_min, status: .services.goroutine_tracker.stats.status, memory_mb: .checks.memory.alloc_mb}'
    sleep 15
done

echo "=== Monitoring Complete ==="
