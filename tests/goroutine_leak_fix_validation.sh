#!/bin/bash

# Goroutine Leak Fix Validation Script
# This script validates the fix for the goroutine leak under high load

set -e

echo "=========================================="
echo "GOROUTINE LEAK FIX VALIDATION"
echo "=========================================="
echo ""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
FASE3_CONTAINERS=8
FASE6_CONTAINERS=55
TARGET_GROWTH_RATE=2.0
MONITORING_DURATION=3600  # 60 minutes

# Function to check goroutine count
get_goroutine_count() {
    local url=$1
    curl -s "$url/debug/pprof/goroutine?debug=1" | grep -c "^goroutine" || echo "0"
}

# Function to run validation
validate_fix() {
    local phase=$1
    local num_containers=$2
    local test_duration=$3

    echo -e "${YELLOW}=========================================="
    echo "TESTING: $phase with $num_containers containers"
    echo -e "==========================================${NC}"
    echo ""

    # Start monitoring
    initial_count=$(get_goroutine_count "http://localhost:6060")
    echo "Initial goroutine count: $initial_count"

    # Record start time
    start_time=$(date +%s)

    # Monitor for specified duration
    local interval=60  # 1 minute intervals
    local elapsed=0
    local samples=()
    local timestamps=()

    echo ""
    echo "Monitoring goroutines for $test_duration seconds..."
    echo ""

    while [ $elapsed -lt $test_duration ]; do
        sleep $interval
        elapsed=$((elapsed + interval))

        current_count=$(get_goroutine_count "http://localhost:6060")
        samples+=($current_count)
        timestamps+=($elapsed)

        minutes=$((elapsed / 60))
        echo "[$minutes min] Goroutines: $current_count"

        # Check if growing too fast
        if [ ${#samples[@]} -gt 5 ]; then
            # Calculate growth rate for last 5 minutes
            start_idx=$((${#samples[@]} - 6))
            start_count=${samples[$start_idx]}
            growth=$((current_count - start_count))
            growth_rate=$(echo "scale=2; $growth / 5.0" | bc)

            echo "  └─ Growth rate (last 5 min): $growth_rate goroutines/min"

            # Early fail if growth rate exceeds target by 2x
            early_fail_threshold=$(echo "scale=2; $TARGET_GROWTH_RATE * 2" | bc)
            if (( $(echo "$growth_rate > $early_fail_threshold" | bc -l) )); then
                echo -e "${RED}EARLY FAIL: Growth rate $growth_rate exceeds 2x threshold ($early_fail_threshold)${NC}"
                return 1
            fi
        fi
    done

    # Calculate final growth rate
    final_count=$(get_goroutine_count "http://localhost:6060")
    total_growth=$((final_count - initial_count))
    duration_minutes=$((test_duration / 60))
    growth_rate=$(echo "scale=2; $total_growth / $duration_minutes" | bc)

    echo ""
    echo "=========================================="
    echo "RESULTS for $phase"
    echo "=========================================="
    echo "Initial goroutines: $initial_count"
    echo "Final goroutines: $final_count"
    echo "Total growth: $total_growth"
    echo "Duration: $duration_minutes minutes"
    echo "Growth rate: $growth_rate goroutines/min"
    echo "Target: < $TARGET_GROWTH_RATE goroutines/min"
    echo ""

    # Check if passed
    if (( $(echo "$growth_rate < $TARGET_GROWTH_RATE" | bc -l) )); then
        echo -e "${GREEN}✅ PASS: Growth rate $growth_rate is below target $TARGET_GROWTH_RATE${NC}"
        return 0
    else
        echo -e "${RED}❌ FAIL: Growth rate $growth_rate exceeds target $TARGET_GROWTH_RATE${NC}"
        return 1
    fi
}

# Main execution
echo "Step 1: Building the fixed binary..."
cd /home/mateus/log_capturer_go
go build -o bin/log_capturer cmd/main.go
echo -e "${GREEN}✅ Build successful${NC}"
echo ""

echo "Step 2: Preparing test environment..."
# Make sure any previous test is stopped
docker-compose down 2>/dev/null || true
echo ""

echo "Step 3: Starting FASE 3 test (8 containers)..."
# This should pass both before and after fix
export TEST_CONTAINERS=$FASE3_CONTAINERS
docker-compose up -d

# Wait for startup
echo "Waiting 30s for startup..."
sleep 30

if validate_fix "FASE 3" $FASE3_CONTAINERS 600; then
    fase3_result="PASS"
else
    fase3_result="FAIL"
fi

# Stop FASE 3
echo ""
echo "Stopping FASE 3 test..."
docker-compose down
sleep 10

echo ""
echo "Step 4: Starting FASE 6 test (55 containers)..."
# This should fail before fix, pass after fix
export TEST_CONTAINERS=$FASE6_CONTAINERS
docker-compose up -d

# Wait for startup
echo "Waiting 60s for startup..."
sleep 60

if validate_fix "FASE 6" $FASE6_CONTAINERS 3600; then
    fase6_result="PASS"
else
    fase6_result="FAIL"
fi

# Stop FASE 6
echo ""
echo "Stopping FASE 6 test..."
docker-compose down

# Final summary
echo ""
echo "=========================================="
echo "FINAL VALIDATION SUMMARY"
echo "=========================================="
echo ""
echo "FASE 3 (8 containers):  $fase3_result"
echo "FASE 6 (55 containers): $fase6_result"
echo ""

if [ "$fase3_result" = "PASS" ] && [ "$fase6_result" = "PASS" ]; then
    echo -e "${GREEN}✅✅✅ FIX VALIDATED SUCCESSFULLY ✅✅✅${NC}"
    echo ""
    echo "The goroutine leak fix has been validated:"
    echo "  - FASE 3 still passes (no regression)"
    echo "  - FASE 6 now passes (leak fixed)"
    exit 0
else
    echo -e "${RED}❌ FIX VALIDATION FAILED ❌${NC}"
    echo ""
    if [ "$fase3_result" = "FAIL" ]; then
        echo "  - FASE 3 regression detected"
    fi
    if [ "$fase6_result" = "FAIL" ]; then
        echo "  - FASE 6 still leaking goroutines"
    fi
    exit 1
fi
