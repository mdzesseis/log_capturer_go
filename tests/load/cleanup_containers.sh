#!/bin/bash
# FASE 5: Load Test - Container Cleanup
# Removes all load test containers
# Author: docker-specialist

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}FASE 5: Load Test Cleanup${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Find all load test containers
CONTAINERS=$(docker ps -a --filter "label=load-test=fase5" --format '{{.Names}}')

if [ -z "$CONTAINERS" ]; then
    echo -e "${YELLOW}No load test containers found.${NC}"
    exit 0
fi

COUNT=$(echo "$CONTAINERS" | wc -l)
echo -e "${YELLOW}Found $COUNT load test containers to remove...${NC}"
echo ""

REMOVED=0
FAILED=0

for CONTAINER in $CONTAINERS; do
    if docker rm -f "$CONTAINER" &>/dev/null; then
        REMOVED=$((REMOVED + 1))
        echo -e "${GREEN}[$REMOVED/$COUNT] Removed: $CONTAINER${NC}"
    else
        FAILED=$((FAILED + 1))
        echo -e "${RED}Failed to remove: $CONTAINER${NC}"
    fi
done

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}Cleanup Summary:${NC}"
echo "  Successfully removed: $REMOVED"
echo "  Failed: $FAILED"
echo "  Total: $COUNT"
echo -e "${BLUE}========================================${NC}"

# Verify cleanup
sleep 1
REMAINING=$(docker ps -a --filter "label=load-test=fase5" --format '{{.Names}}' | wc -l)

if [ "$REMAINING" -eq 0 ]; then
    echo -e "${GREEN}SUCCESS: All load test containers removed!${NC}"
    exit 0
else
    echo -e "${YELLOW}WARNING: $REMAINING containers still remaining${NC}"
    exit 1
fi
