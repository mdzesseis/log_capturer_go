#!/bin/bash
# =============================================================================
# Log Capturer - Comprehensive Health Check Script
# =============================================================================
# Usage: ./health_check.sh [--verbose] [--output FILE]
# =============================================================================

set -euo pipefail

# Configuration
HEALTH_ENDPOINT="${HEALTH_ENDPOINT:-http://localhost:8401/health}"
METRICS_ENDPOINT="${METRICS_ENDPOINT:-http://localhost:8001/metrics}"
VERBOSE=0
OUTPUT_FILE=""

# Thresholds
GOROUTINE_THRESHOLD=5000
QUEUE_THRESHOLD_PERCENT=70
DLQ_THRESHOLD=5000
MEMORY_THRESHOLD_PERCENT=70
FD_THRESHOLD_PERCENT=70

# Colors
RED='\033[0:31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --verbose)
      VERBOSE=1
      shift
      ;;
    --output)
      OUTPUT_FILE="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Logging function
log() {
  local level="$1"
  shift
  local message="$*"

  case "$level" in
    INFO)
      echo -e "${GREEN}[INFO]${NC} $message"
      ;;
    WARN)
      echo -e "${YELLOW}[WARN]${NC} $message"
      ;;
    ERROR)
      echo -e "${RED}[ERROR]${NC} $message"
      ;;
    *)
      echo "$message"
      ;;
  esac

  if [[ -n "$OUTPUT_FILE" ]]; then
    echo "[$level] $message" >> "$OUTPUT_FILE"
  fi
}

# Fetch metric value
get_metric() {
  local metric_name="$1"
  curl -s "$METRICS_ENDPOINT" | grep "^$metric_name" | awk '{print $2}' | head -1
}

# Health check
echo "========================================="
echo "  Log Capturer - Health Check Report"
echo "  $(date)"
echo "========================================="
echo ""

# 1. Service Status
log "" "1. SERVICE STATUS"
log "" "  -----------------------------------------"
if docker ps | grep -q log_capturer; then
  log "INFO" "  Service: RUNNING"
else
  log "ERROR" "  Service: DOWN"
  exit 1
fi

# 2. Health Endpoint
log "" ""
log "" "2. HEALTH ENDPOINT"
log "" "  -----------------------------------------"
HEALTH_STATUS=$(curl -s "$HEALTH_ENDPOINT" | jq -r '.status' 2>/dev/null || echo "UNKNOWN")
if [[ "$HEALTH_STATUS" == "healthy" ]]; then
  log "INFO" "  Health Status: $HEALTH_STATUS"
else
  log "ERROR" "  Health Status: $HEALTH_STATUS"
fi

# Component health
COMPONENTS=$(curl -s "$HEALTH_ENDPOINT" | jq -r '.services | keys[]' 2>/dev/null || echo "")
for component in $COMPONENTS; do
  COMPONENT_STATUS=$(curl -s "$HEALTH_ENDPOINT" | jq -r ".services.$component.status" 2>/dev/null || echo "unknown")
  if [[ "$COMPONENT_STATUS" == "healthy" ]]; then
    log "INFO" "    - $component: $COMPONENT_STATUS"
  else
    log "WARN" "    - $component: $COMPONENT_STATUS"
  fi
done

# 3. Goroutines
log "" ""
log "" "3. GOROUTINE COUNT"
log "" "  -----------------------------------------"
GOROUTINES=$(get_metric "go_goroutines")
if [[ -n "$GOROUTINES" ]]; then
  if (( $(echo "$GOROUTINES > $GOROUTINE_THRESHOLD" | bc -l) )); then
    log "ERROR" "  Goroutines: $GOROUTINES (CRITICAL - threshold: $GOROUTINE_THRESHOLD)"
  elif (( $(echo "$GOROUTINES > 3000" | bc -l) )); then
    log "WARN" "  Goroutines: $GOROUTINES (WARNING - threshold: 3000)"
  else
    log "INFO" "  Goroutines: $GOROUTINES (OK)"
  fi
else
  log "ERROR" "  Goroutines: Unable to fetch"
fi

# 4. Memory Usage
log "" ""
log "" "4. MEMORY USAGE"
log "" "  -----------------------------------------"
MEMORY_BYTES=$(get_metric "process_resident_memory_bytes")
if [[ -n "$MEMORY_BYTES" ]]; then
  MEMORY_MB=$(echo "scale=2; $MEMORY_BYTES / 1048576" | bc)
  log "INFO" "  Memory: ${MEMORY_MB} MB"
else
  log "ERROR" "  Memory: Unable to fetch"
fi

# 5. Queue Utilization
log "" ""
log "" "5. QUEUE UTILIZATION"
log "" "  -----------------------------------------"
QUEUE_SIZE=$(get_metric 'log_capturer_queue_size{component="dispatcher"')
if [[ -n "$QUEUE_SIZE" ]]; then
  QUEUE_UTILIZATION=$(echo "scale=2; $QUEUE_SIZE / 50000 * 100" | bc)
  if (( $(echo "$QUEUE_UTILIZATION > 90" | bc -l) )); then
    log "ERROR" "  Dispatcher Queue: ${QUEUE_SIZE} / 50000 (${QUEUE_UTILIZATION}% - CRITICAL)"
  elif (( $(echo "$QUEUE_UTILIZATION > $QUEUE_THRESHOLD_PERCENT" | bc -l) )); then
    log "WARN" "  Dispatcher Queue: ${QUEUE_SIZE} / 50000 (${QUEUE_UTILIZATION}% - WARNING)"
  else
    log "INFO" "  Dispatcher Queue: ${QUEUE_SIZE} / 50000 (${QUEUE_UTILIZATION}%)"
  fi
else
  log "ERROR" "  Dispatcher Queue: Unable to fetch"
fi

# 6. Logs Processed
log "" ""
log "" "6. LOG PROCESSING"
log "" "  -----------------------------------------"
LOGS_PROCESSED=$(get_metric "log_capturer_logs_processed_total")
if [[ -n "$LOGS_PROCESSED" ]]; then
  log "INFO" "  Total Logs Processed: $LOGS_PROCESSED"
else
  log "ERROR" "  Logs Processed: Unable to fetch"
fi

# 7. Error Rate
log "" ""
log "" "7. ERROR RATE"
log "" "  -----------------------------------------"
ERRORS_TOTAL=$(get_metric "log_capturer_errors_total")
if [[ -n "$ERRORS_TOTAL" ]]; then
  log "INFO" "  Total Errors: $ERRORS_TOTAL"
else
  log "WARN" "  Errors: Unable to fetch"
fi

# 8. Position System
log "" ""
log "" "8. POSITION SYSTEM"
log "" "  -----------------------------------------"
CORRUPTIONS=$(get_metric "log_capturer_position_corruptions_detected_total")
CHECKPOINT_TIMESTAMP=$(get_metric "log_capturer_position_checkpoint_last_saved_timestamp")
POSITION_SAVES=$(get_metric "log_capturer_position_saves_total")

if [[ -n "$CORRUPTIONS" ]] && [[ "$CORRUPTIONS" != "0" ]]; then
  log "ERROR" "  Position Corruptions: $CORRUPTIONS (CRITICAL)"
else
  log "INFO" "  Position Corruptions: ${CORRUPTIONS:-0} (OK)"
fi

if [[ -n "$CHECKPOINT_TIMESTAMP" ]]; then
  CHECKPOINT_AGE=$(( $(date +%s) - ${CHECKPOINT_TIMESTAMP%.*} ))
  if (( CHECKPOINT_AGE > 600 )); then
    log "WARN" "  Last Checkpoint: ${CHECKPOINT_AGE}s ago (WARNING - expected every 300s)"
  else
    log "INFO" "  Last Checkpoint: ${CHECKPOINT_AGE}s ago (OK)"
  fi
else
  log "WARN" "  Checkpoint Timestamp: Unable to fetch"
fi

log "INFO" "  Total Position Saves: ${POSITION_SAVES:-unknown}"

# 9. DLQ Status
log "" ""
log "" "9. DEAD LETTER QUEUE (DLQ)"
log "" "  -----------------------------------------"
DLQ_ENTRIES=$(get_metric "log_capturer_dlq_entries_total")
DLQ_QUEUE_SIZE=$(get_metric 'log_capturer_queue_size{component="dlq"')

if [[ -n "$DLQ_ENTRIES" ]]; then
  if (( $(echo "$DLQ_ENTRIES > 10000" | bc -l) )); then
    log "ERROR" "  DLQ Entries: $DLQ_ENTRIES (CRITICAL - threshold: 10000)"
  elif (( $(echo "$DLQ_ENTRIES > $DLQ_THRESHOLD" | bc -l) )); then
    log "WARN" "  DLQ Entries: $DLQ_ENTRIES (WARNING - threshold: $DLQ_THRESHOLD)"
  else
    log "INFO" "  DLQ Entries: $DLQ_ENTRIES (OK)"
  fi
else
  log "INFO" "  DLQ Entries: ${DLQ_ENTRIES:-0} (OK)"
fi

if [[ -n "$DLQ_QUEUE_SIZE" ]]; then
  log "INFO" "  DLQ Queue Size: $DLQ_QUEUE_SIZE"
else
  log "INFO" "  DLQ Queue Size: 0"
fi

# 10. Deduplication
log "" ""
log "" "10. DEDUPLICATION SYSTEM"
log "" "  -----------------------------------------"
DEDUP_RATE=$(get_metric "log_capturer_deduplication_duplicate_rate")
DEDUP_CACHE_SIZE=$(get_metric "log_capturer_deduplication_cache_size")
DEDUP_HIT_RATE=$(get_metric "log_capturer_deduplication_cache_hit_rate")

if [[ -n "$DEDUP_RATE" ]]; then
  DEDUP_RATE_PERCENT=$(echo "scale=2; $DEDUP_RATE * 100" | bc)
  if (( $(echo "$DEDUP_RATE > 0.30" | bc -l) )); then
    log "WARN" "  Duplicate Rate: ${DEDUP_RATE_PERCENT}% (WARNING - expected < 30%)"
  else
    log "INFO" "  Duplicate Rate: ${DEDUP_RATE_PERCENT}% (OK)"
  fi
else
  log "INFO" "  Duplicate Rate: N/A"
fi

log "INFO" "  Cache Size: ${DEDUP_CACHE_SIZE:-0} / 100000"

if [[ -n "$DEDUP_HIT_RATE" ]]; then
  DEDUP_HIT_RATE_PERCENT=$(echo "scale=2; $DEDUP_HIT_RATE * 100" | bc)
  log "INFO" "  Cache Hit Rate: ${DEDUP_HIT_RATE_PERCENT}% (expected 70-90%)"
fi

# 11. Timestamp Learning
log "" ""
log "" "11. TIMESTAMP LEARNING"
log "" "  -----------------------------------------"
TIMESTAMP_REJECTIONS=$(get_metric "log_capturer_timestamp_rejection_total")
TIMESTAMP_MAX_AGE=$(get_metric "log_capturer_timestamp_max_acceptable_age_seconds")

log "INFO" "  Total Rejections: ${TIMESTAMP_REJECTIONS:-0}"
if [[ -n "$TIMESTAMP_MAX_AGE" ]]; then
  TIMESTAMP_MAX_AGE_HOURS=$(echo "scale=2; $TIMESTAMP_MAX_AGE / 3600" | bc)
  log "INFO" "  Max Acceptable Age: ${TIMESTAMP_MAX_AGE_HOURS}h"
fi

# 12. File Descriptors
log "" ""
log "" "12. FILE DESCRIPTORS"
log "" "  -----------------------------------------"
FD_OPEN=$(get_metric "process_open_fds")
FD_MAX=$(get_metric "process_max_fds")

if [[ -n "$FD_OPEN" ]] && [[ -n "$FD_MAX" ]]; then
  FD_PERCENT=$(echo "scale=2; $FD_OPEN / $FD_MAX * 100" | bc)
  if (( $(echo "$FD_PERCENT > 80" | bc -l) )); then
    log "ERROR" "  File Descriptors: ${FD_OPEN} / ${FD_MAX} (${FD_PERCENT}% - CRITICAL)"
  elif (( $(echo "$FD_PERCENT > $FD_THRESHOLD_PERCENT" | bc -l) )); then
    log "WARN" "  File Descriptors: ${FD_OPEN} / ${FD_MAX} (${FD_PERCENT}% - WARNING)"
  else
    log "INFO" "  File Descriptors: ${FD_OPEN} / ${FD_MAX} (${FD_PERCENT}%)"
  fi
else
  log "WARN" "  File Descriptors: Unable to fetch"
fi

# Summary
echo ""
echo "========================================="
echo "  Health Check Complete"
echo "========================================="
log "INFO" "Timestamp: $(date)"
if [[ -n "$OUTPUT_FILE" ]]; then
  log "INFO" "Full report saved to: $OUTPUT_FILE"
fi

echo ""
echo "Recommendations:"
echo "  - Review any ERROR or WARN entries above"
echo "  - Check Grafana dashboards for trends"
echo "  - Consult /home/mateus/log_capturer_go/docs/RECOVERY_RUNBOOK.md for recovery procedures"
echo ""
