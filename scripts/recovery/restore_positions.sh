#!/bin/bash
# =============================================================================
# Log Capturer - Restore Position File from Checkpoint
# =============================================================================
# Usage: ./restore_positions.sh [--generation N] [--dry-run]
# =============================================================================

set -euo pipefail

# Configuration
POSITION_DIR="${POSITION_DIR:-/app/data/positions}"
CHECKPOINT_DIR="$POSITION_DIR/checkpoints"
POSITION_FILE="$POSITION_DIR/positions.json"
DRY_RUN=0
GENERATION=0

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --generation)
      GENERATION="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--generation N] [--dry-run]"
      exit 1
      ;;
  esac
done

echo "========================================="
echo "  Position File Restoration"
echo "========================================="
echo ""

# Check if service is running
if docker ps | grep -q log_capturer; then
  echo "ERROR: Service is still running!"
  echo "Please stop the service first: docker-compose stop log_capturer"
  exit 1
fi

# List available checkpoints
echo "Available checkpoints:"
ls -lh "$CHECKPOINT_DIR"/ 2>/dev/null || {
  echo "ERROR: No checkpoints found in $CHECKPOINT_DIR"
  exit 1
}
echo ""

# Select checkpoint
CHECKPOINT_FILE="$CHECKPOINT_DIR/positions_checkpoint_gen_${GENERATION}.json"

if [[ ! -f "$CHECKPOINT_FILE" ]]; then
  echo "ERROR: Checkpoint generation $GENERATION not found: $CHECKPOINT_FILE"
  echo ""
  echo "Available generations:"
  ls -1 "$CHECKPOINT_DIR"/positions_checkpoint_gen_*.json 2>/dev/null | sed 's/.*gen_/  - Generation /' | sed 's/.json$//'
  exit 1
fi

echo "Selected checkpoint: $CHECKPOINT_FILE"
CHECKPOINT_SIZE=$(stat -f%z "$CHECKPOINT_FILE" 2>/dev/null || stat -c%s "$CHECKPOINT_FILE" 2>/dev/null)
CHECKPOINT_AGE=$(( $(date +%s) - $(stat -f%m "$CHECKPOINT_FILE" 2>/dev/null || stat -c%Y "$CHECKPOINT_FILE" 2>/dev/null) ))
echo "  - Size: $(echo "scale=2; $CHECKPOINT_SIZE / 1024" | bc) KB"
echo "  - Age: ${CHECKPOINT_AGE}s ($(date -d "@$(stat -c%Y "$CHECKPOINT_FILE" 2>/dev/null || stat -f%m "$CHECKPOINT_FILE" 2>/dev/null)" '+%Y-%m-%d %H:%M:%S' 2>/dev/null || echo 'unknown'))"
echo ""

# Validate checkpoint JSON
echo "Validating checkpoint JSON..."
if ! jq '.' "$CHECKPOINT_FILE" > /dev/null 2>&1; then
  echo "ERROR: Checkpoint is not valid JSON!"
  exit 1
fi
echo "  ✓ Checkpoint is valid JSON"
echo ""

# Show checkpoint contents (first few positions)
echo "Checkpoint contents (sample):"
jq -r '.positions | to_entries | .[:3] | .[] | "  - \(.key): offset \(.value.offset), inode \(.value.inode)"' "$CHECKPOINT_FILE" 2>/dev/null || echo "  (Unable to parse positions)"
TOTAL_POSITIONS=$(jq '.positions | length' "$CHECKPOINT_FILE" 2>/dev/null || echo "unknown")
echo "  - Total positions: $TOTAL_POSITIONS"
echo ""

# Backup current position file (if exists)
if [[ -f "$POSITION_FILE" ]]; then
  BACKUP_FILE="${POSITION_FILE}.backup.$(date +%s)"
  echo "Backing up current position file..."
  if [[ $DRY_RUN -eq 1 ]]; then
    echo "  [DRY-RUN] Would backup: $POSITION_FILE → $BACKUP_FILE"
  else
    cp "$POSITION_FILE" "$BACKUP_FILE"
    echo "  ✓ Backup created: $BACKUP_FILE"
  fi
  echo ""
fi

# Restore checkpoint
echo "Restoring checkpoint to position file..."
if [[ $DRY_RUN -eq 1 ]]; then
  echo "  [DRY-RUN] Would restore: $CHECKPOINT_FILE → $POSITION_FILE"
else
  cp "$CHECKPOINT_FILE" "$POSITION_FILE"
  echo "  ✓ Position file restored"
fi
echo ""

# Validate restored position file
if [[ $DRY_RUN -eq 0 ]]; then
  echo "Validating restored position file..."
  if ! jq '.' "$POSITION_FILE" > /dev/null 2>&1; then
    echo "ERROR: Restored position file is not valid JSON!"
    echo "Rolling back..."
    if [[ -f "$BACKUP_FILE" ]]; then
      cp "$BACKUP_FILE" "$POSITION_FILE"
      echo "  ✓ Rolled back to backup"
    fi
    exit 1
  fi
  echo "  ✓ Restored position file is valid JSON"
  echo ""
fi

# Summary
echo "========================================="
echo "  Restoration Complete"
echo "========================================="
echo ""
echo "Next steps:"
echo "  1. Start the service: docker-compose start log_capturer"
echo "  2. Monitor logs: docker logs -f log_capturer_go"
echo "  3. Verify position saves working: curl http://localhost:8001/metrics | grep log_capturer_position_saves_total"
echo "  4. Check for replayed logs (deduplication should catch them)"
echo ""

if [[ $DRY_RUN -eq 1 ]]; then
  echo "NOTE: This was a DRY RUN. No changes were made."
  echo "      Run without --dry-run to perform actual restoration."
  echo ""
fi
