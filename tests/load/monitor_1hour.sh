#!/bin/bash
# FASE 5: Load Test - Extended Monitoring (1 hour)
# Monitors system metrics every 2 minutes for 60 minutes
# Author: observability

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m'

# Configuration
DURATION_MINUTES=${1:-60}
CHECK_INTERVAL_SECONDS=120  # 2 minutes
TOTAL_CHECKS=$((DURATION_MINUTES * 60 / CHECK_INTERVAL_SECONDS))
LOG_FILE="phase5_load_test.log"
METRICS_FILE="phase5_metrics.csv"
API_BASE="http://localhost:8401"
PROMETHEUS_BASE="http://localhost:9090"

# Initialize log file
echo "FASE 5 Load Test - Started at $(date)" > "$LOG_FILE"
echo "Duration: $DURATION_MINUTES minutes" >> "$LOG_FILE"
echo "Check interval: $CHECK_INTERVAL_SECONDS seconds" >> "$LOG_FILE"
echo "Total checks: $TOTAL_CHECKS" >> "$LOG_FILE"
echo "========================================" >> "$LOG_FILE"
echo "" >> "$LOG_FILE"

# Initialize CSV file
echo "timestamp,check_number,elapsed_minutes,goroutines,active_streams,rotations,file_descriptors,memory_mb,cpu_percent,logs_processed,health_status,container_count" > "$METRICS_FILE"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}FASE 5: Extended Monitoring (1 Hour)${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "${GREEN}Configuration:${NC}"
echo "  Duration: $DURATION_MINUTES minutes"
echo "  Check interval: $CHECK_INTERVAL_SECONDS seconds"
echo "  Total checks: $TOTAL_CHECKS"
echo "  Log file: $LOG_FILE"
echo "  Metrics file: $METRICS_FILE"
echo ""

START_TIME=$(date +%s)
CHECK_COUNT=0

# Arrays to store historical data for trend analysis
declare -a GOROUTINE_HISTORY
declare -a STREAM_HISTORY
declare -a MEMORY_HISTORY
declare -a FD_HISTORY

function query_prometheus() {
    local query=$1
    curl -s "${PROMETHEUS_BASE}/api/v1/query?query=${query}" | jq -r '.data.result[0].value[1]' 2>/dev/null || echo "0"
}

function get_health_status() {
    local health=$(curl -s "${API_BASE}/health" 2>/dev/null)
    if [ -z "$health" ]; then
        echo "UNKNOWN"
        return
    fi

    local status=$(echo "$health" | jq -r '.status' 2>/dev/null || echo "UNKNOWN")
    echo "$status"
}

function collect_metrics() {
    local check_num=$1
    local elapsed_min=$2

    # Prometheus metrics
    local goroutines=$(query_prometheus "log_capturer_goroutines")
    local active_streams=$(query_prometheus "log_capturer_container_monitor_active_streams")
    local rotations=$(query_prometheus "log_capturer_container_monitor_stream_rotations_total")
    local memory_bytes=$(query_prometheus "log_capturer_memory_usage_bytes")
    local memory_mb=$(echo "scale=2; $memory_bytes / 1024 / 1024" | bc 2>/dev/null || echo "0")
    local logs_processed=$(query_prometheus "log_capturer_logs_processed_total")

    # File descriptors from /proc
    local fd_count=0
    local pid=$(pgrep -f "log_capturer" | head -1)
    if [ -n "$pid" ]; then
        fd_count=$(ls -1 /proc/$pid/fd 2>/dev/null | wc -l)
    fi

    # CPU usage
    local cpu_percent=0
    if [ -n "$pid" ]; then
        cpu_percent=$(ps -p $pid -o %cpu= 2>/dev/null | tr -d ' ' || echo "0")
    fi

    # Health status
    local health_status=$(get_health_status)

    # Container count
    local container_count=$(docker ps --filter "label=load-test=fase5" --format '{{.Names}}' | wc -l)

    # Store in history
    GOROUTINE_HISTORY+=("$goroutines")
    STREAM_HISTORY+=("$active_streams")
    MEMORY_HISTORY+=("$memory_mb")
    FD_HISTORY+=("$fd_count")

    # Calculate growth rate (last 10 minutes if available)
    local goroutine_growth="N/A"
    if [ ${#GOROUTINE_HISTORY[@]} -ge 5 ]; then
        local old_g=${GOROUTINE_HISTORY[-5]}
        local new_g=$goroutines
        local time_diff=10  # 5 checks * 2 min = 10 min
        goroutine_growth=$(echo "scale=2; ($new_g - $old_g) / $time_diff" | bc 2>/dev/null || echo "N/A")
    fi

    # Write to CSV
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo "$timestamp,$check_num,$elapsed_min,$goroutines,$active_streams,$rotations,$fd_count,$memory_mb,$cpu_percent,$logs_processed,$health_status,$container_count" >> "$METRICS_FILE"

    # Write to log
    {
        echo "[$timestamp] Check $check_num/$TOTAL_CHECKS (${elapsed_min}m elapsed)"
        echo "  Goroutines: $goroutines (growth: ${goroutine_growth}/min)"
        echo "  Active Streams: $active_streams/50"
        echo "  Stream Rotations: $rotations"
        echo "  File Descriptors: $fd_count"
        echo "  Memory: ${memory_mb} MB"
        echo "  CPU: ${cpu_percent}%"
        echo "  Logs Processed: $logs_processed"
        echo "  Health: $health_status"
        echo "  Test Containers: $container_count"
        echo "  ---"
    } >> "$LOG_FILE"

    # Console output
    echo -e "${CYAN}[$timestamp]${NC} Check ${MAGENTA}$check_num/$TOTAL_CHECKS${NC} (${elapsed_min}m)"
    echo -e "  ${GREEN}Goroutines:${NC} $goroutines (${goroutine_growth}/min)"
    echo -e "  ${GREEN}Streams:${NC} $active_streams/50"
    echo -e "  ${GREEN}Rotations:${NC} $rotations"
    echo -e "  ${GREEN}FDs:${NC} $fd_count"
    echo -e "  ${GREEN}Memory:${NC} ${memory_mb} MB"
    echo -e "  ${GREEN}CPU:${NC} ${cpu_percent}%"
    echo -e "  ${GREEN}Health:${NC} $health_status"
    echo -e "  ${GREEN}Containers:${NC} $container_count"

    # Warnings
    if [ "$health_status" != "healthy" ] && [ "$health_status" != "UNKNOWN" ]; then
        echo -e "  ${RED}WARNING: System is $health_status!${NC}"
    fi

    if [ "$active_streams" -ge 50 ]; then
        echo -e "  ${YELLOW}INFO: Stream pool at capacity (50/50)${NC}"
    fi

    if [ $(echo "$goroutine_growth > 2" | bc 2>/dev/null || echo 0) -eq 1 ]; then
        echo -e "  ${RED}WARNING: Goroutine growth rate exceeds 2/min!${NC}"
    fi

    echo ""
}

echo -e "${GREEN}Starting 1-hour monitoring...${NC}"
echo ""

# Main monitoring loop
while [ $CHECK_COUNT -lt $TOTAL_CHECKS ]; do
    CHECK_COUNT=$((CHECK_COUNT + 1))

    CURRENT_TIME=$(date +%s)
    ELAPSED_SECONDS=$((CURRENT_TIME - START_TIME))
    ELAPSED_MINUTES=$((ELAPSED_SECONDS / 60))

    collect_metrics $CHECK_COUNT $ELAPSED_MINUTES

    # Don't sleep after last check
    if [ $CHECK_COUNT -lt $TOTAL_CHECKS ]; then
        # Show countdown
        REMAINING_CHECKS=$((TOTAL_CHECKS - CHECK_COUNT))
        REMAINING_MINUTES=$((REMAINING_CHECKS * CHECK_INTERVAL_SECONDS / 60))
        echo -e "${BLUE}Next check in ${CHECK_INTERVAL_SECONDS}s... ($REMAINING_CHECKS checks, ~${REMAINING_MINUTES}m remaining)${NC}"
        echo ""
        sleep $CHECK_INTERVAL_SECONDS
    fi
done

END_TIME=$(date +%s)
TOTAL_DURATION=$((END_TIME - START_TIME))
TOTAL_MINUTES=$((TOTAL_DURATION / 60))

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}Monitoring Complete!${NC}"
echo -e "${BLUE}========================================${NC}"
echo "  Total duration: ${TOTAL_MINUTES}m (${TOTAL_DURATION}s)"
echo "  Total checks: $CHECK_COUNT"
echo "  Log file: $LOG_FILE"
echo "  Metrics file: $METRICS_FILE"
echo -e "${BLUE}========================================${NC}"

# Generate summary statistics
echo "" >> "$LOG_FILE"
echo "========================================" >> "$LOG_FILE"
echo "SUMMARY STATISTICS" >> "$LOG_FILE"
echo "========================================" >> "$LOG_FILE"

# Calculate statistics from history
if [ ${#GOROUTINE_HISTORY[@]} -gt 0 ]; then
    local min_g=$(printf '%s\n' "${GOROUTINE_HISTORY[@]}" | sort -n | head -1)
    local max_g=$(printf '%s\n' "${GOROUTINE_HISTORY[@]}" | sort -n | tail -1)
    local first_g=${GOROUTINE_HISTORY[0]}
    local last_g=${GOROUTINE_HISTORY[-1]}
    local total_growth=$((last_g - first_g))
    local avg_growth=$(echo "scale=2; $total_growth / $TOTAL_MINUTES" | bc)

    echo "Goroutines:" >> "$LOG_FILE"
    echo "  Initial: $first_g" >> "$LOG_FILE"
    echo "  Final: $last_g" >> "$LOG_FILE"
    echo "  Min: $min_g" >> "$LOG_FILE"
    echo "  Max: $max_g" >> "$LOG_FILE"
    echo "  Total growth: $total_growth" >> "$LOG_FILE"
    echo "  Avg growth rate: ${avg_growth}/min" >> "$LOG_FILE"
    echo "" >> "$LOG_FILE"
fi

if [ ${#STREAM_HISTORY[@]} -gt 0 ]; then
    local max_streams=$(printf '%s\n' "${STREAM_HISTORY[@]}" | sort -n | tail -1)
    echo "Active Streams:" >> "$LOG_FILE"
    echo "  Peak: $max_streams/50" >> "$LOG_FILE"
    echo "" >> "$LOG_FILE"
fi

if [ ${#MEMORY_HISTORY[@]} -gt 0 ]; then
    local min_mem=$(printf '%s\n' "${MEMORY_HISTORY[@]}" | sort -n | head -1)
    local max_mem=$(printf '%s\n' "${MEMORY_HISTORY[@]}" | sort -n | tail -1)
    echo "Memory (MB):" >> "$LOG_FILE"
    echo "  Min: $min_mem" >> "$LOG_FILE"
    echo "  Max: $max_mem" >> "$LOG_FILE"
    echo "" >> "$LOG_FILE"
fi

echo "Test completed at $(date)" >> "$LOG_FILE"

echo -e "${GREEN}Summary statistics written to $LOG_FILE${NC}"
