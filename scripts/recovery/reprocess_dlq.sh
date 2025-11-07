#!/bin/bash
# =============================================================================
# Log Capturer - Manual DLQ Reprocessing Script
# =============================================================================
# Usage: ./reprocess_dlq.sh [--batch-size N] [--dry-run]
# =============================================================================

set -euo pipefail

# Configuration
DLQ_DIR="${DLQ_DIR:-/tmp/dlq}"
LOKI_URL="${LOKI_URL:-http://loki:3100}"
BATCH_SIZE=10
DRY_RUN=0
PROCESSED=0
FAILED=0
SKIPPED=0

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --batch-size)
      BATCH_SIZE="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--batch-size N] [--dry-run]"
      exit 1
      ;;
  esac
done

echo "========================================="
echo "  DLQ Manual Reprocessing"
echo "========================================="
echo ""

# Check DLQ directory
if [[ ! -d "$DLQ_DIR" ]]; then
  echo "ERROR: DLQ directory not found: $DLQ_DIR"
  exit 1
fi

# Count DLQ entries
TOTAL_ENTRIES=$(find "$DLQ_DIR" -name "*.json" | wc -l)
echo "DLQ Status:"
echo "  - Directory: $DLQ_DIR"
echo "  - Total entries: $TOTAL_ENTRIES"
echo "  - Batch size: $BATCH_SIZE"
echo ""

if [[ $TOTAL_ENTRIES -eq 0 ]]; then
  echo "No entries to reprocess. Exiting."
  exit 0
fi

# Check Loki connectivity
echo "Checking Loki connectivity..."
if curl -s -f "$LOKI_URL/ready" > /dev/null 2>&1; then
  echo "  ✓ Loki is reachable at $LOKI_URL"
else
  echo "  ✗ ERROR: Loki is not reachable at $LOKI_URL"
  echo ""
  echo "Please check:"
  echo "  1. Loki service is running: docker ps | grep loki"
  echo "  2. Network connectivity: docker network inspect log_capturer_go_default"
  echo "  3. Loki endpoint: $LOKI_URL/ready"
  exit 1
fi
echo ""

# Confirm reprocessing
if [[ $DRY_RUN -eq 0 ]]; then
  echo "WARNING: This will send $TOTAL_ENTRIES entries to Loki."
  echo "         Press Ctrl+C to cancel, or Enter to continue..."
  read -r
  echo ""
fi

# Process DLQ entries
echo "Processing DLQ entries..."
echo ""

BATCH_COUNT=0
for dlq_file in "$DLQ_DIR"/*.json; do
  [[ -f "$dlq_file" ]] || continue

  BATCH_COUNT=$((BATCH_COUNT + 1))
  FILE_NAME=$(basename "$dlq_file")

  # Validate JSON
  if ! jq '.' "$dlq_file" > /dev/null 2>&1; then
    echo "  [$BATCH_COUNT/$TOTAL_ENTRIES] SKIP $FILE_NAME (invalid JSON)"
    SKIPPED=$((SKIPPED + 1))
    continue
  fi

  # Send to Loki
  if [[ $DRY_RUN -eq 1 ]]; then
    echo "  [$BATCH_COUNT/$TOTAL_ENTRIES] [DRY-RUN] Would send: $FILE_NAME"
    PROCESSED=$((PROCESSED + 1))
  else
    if curl -s -X POST \
      -H "Content-Type: application/json" \
      -d @"$dlq_file" \
      "$LOKI_URL/loki/api/v1/push" \
      > /dev/null 2>&1; then
      echo "  [$BATCH_COUNT/$TOTAL_ENTRIES] SUCCESS: $FILE_NAME"
      PROCESSED=$((PROCESSED + 1))
      # Archive successfully processed entry
      ARCHIVE_DIR="$DLQ_DIR/processed_$(date +%Y%m%d)"
      mkdir -p "$ARCHIVE_DIR"
      mv "$dlq_file" "$ARCHIVE_DIR/"
    else
      echo "  [$BATCH_COUNT/$TOTAL_ENTRIES] FAILED: $FILE_NAME"
      FAILED=$((FAILED + 1))
    fi
  fi

  # Batch pause
  if (( BATCH_COUNT % BATCH_SIZE == 0 )); then
    echo ""
    echo "  Batch complete ($BATCH_COUNT processed). Pausing 2s..."
    sleep 2
    echo ""
  fi
done

# Summary
echo ""
echo "========================================="
echo "  Reprocessing Complete"
echo "========================================="
echo ""
echo "Results:"
echo "  - Total entries: $TOTAL_ENTRIES"
echo "  - Successfully processed: $PROCESSED"
echo "  - Failed: $FAILED"
echo "  - Skipped (invalid): $SKIPPED"
echo ""

if [[ $DRY_RUN -eq 1 ]]; then
  echo "NOTE: This was a DRY RUN. No entries were sent to Loki."
  echo "      Run without --dry-run to perform actual reprocessing."
  echo ""
fi

if [[ $PROCESSED -gt 0 ]] && [[ $DRY_RUN -eq 0 ]]; then
  echo "Successfully processed entries have been archived to:"
  echo "  $DLQ_DIR/processed_$(date +%Y%m%d)/"
  echo ""
fi

if [[ $FAILED -gt 0 ]]; then
  echo "WARNING: $FAILED entries failed to process."
  echo "         Check Loki logs for errors."
  echo ""
fi

# Check metrics
if [[ $DRY_RUN -eq 0 ]]; then
  echo "Current DLQ metrics:"
  curl -s http://localhost:8001/metrics | grep "log_capturer_dlq_entries_total" || echo "  (Unable to fetch metrics)"
  echo ""
fi
