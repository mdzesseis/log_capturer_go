#!/bin/bash
# FASE 5: Load Test - Container Spawner
# Spawns N containers for load testing log_capturer stream pool
# Author: docker-specialist + qa-specialist

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default configuration
CONTAINER_COUNT=${1:-55}
CONTAINER_PREFIX="load-test-container"
NETWORK="log_capturer_go_default"
LOG_INTERVAL=1  # seconds between log entries

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}FASE 5: Load Test Container Spawner${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "${GREEN}Configuration:${NC}"
echo "  Container count: $CONTAINER_COUNT"
echo "  Container prefix: $CONTAINER_PREFIX"
echo "  Network: $NETWORK"
echo "  Log interval: ${LOG_INTERVAL}s"
echo ""

# Check if network exists
if ! docker network inspect "$NETWORK" &>/dev/null; then
    echo -e "${YELLOW}Warning: Network '$NETWORK' not found, creating it...${NC}"
    docker network create "$NETWORK"
fi

echo -e "${BLUE}Spawning $CONTAINER_COUNT containers...${NC}"

SPAWNED=0
FAILED=0

for i in $(seq 1 $CONTAINER_COUNT); do
    CONTAINER_NAME="${CONTAINER_PREFIX}-${i}"

    # Check if container already exists
    if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
        echo -e "${YELLOW}Container $CONTAINER_NAME already exists, removing...${NC}"
        docker rm -f "$CONTAINER_NAME" &>/dev/null || true
    fi

    # Spawn container with continuous logging
    if docker run -d \
        --name "$CONTAINER_NAME" \
        --network "$NETWORK" \
        --label "load-test=fase5" \
        --label "test-batch=$(date +%s)" \
        alpine sh -c "
        COUNTER=0
        while true; do
            COUNTER=\$((COUNTER + 1))
            TIMESTAMP=\$(date '+%Y-%m-%d %H:%M:%S')
            RANDOM_VALUE=\$((RANDOM % 1000))
            echo \"\$TIMESTAMP - [LOAD-TEST] Container $i - Entry \$COUNTER - Value: \$RANDOM_VALUE - $(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1)\"
            sleep $LOG_INTERVAL
        done
        " &>/dev/null; then

        SPAWNED=$((SPAWNED + 1))
        echo -e "${GREEN}[$SPAWNED/$CONTAINER_COUNT] Spawned: $CONTAINER_NAME${NC}"
    else
        FAILED=$((FAILED + 1))
        echo -e "${RED}Failed to spawn: $CONTAINER_NAME${NC}"
    fi
done

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}Spawn Summary:${NC}"
echo "  Successfully spawned: $SPAWNED"
echo "  Failed: $FAILED"
echo "  Total: $CONTAINER_COUNT"
echo -e "${BLUE}========================================${NC}"

# Verify containers are running
sleep 2
RUNNING=$(docker ps --filter "label=load-test=fase5" --format '{{.Names}}' | wc -l)
echo ""
echo -e "${GREEN}Verification:${NC}"
echo "  Running containers: $RUNNING"

if [ "$RUNNING" -ge "$CONTAINER_COUNT" ]; then
    echo -e "${GREEN}SUCCESS: All containers are running!${NC}"
    exit 0
elif [ "$RUNNING" -ge $((CONTAINER_COUNT * 90 / 100)) ]; then
    echo -e "${YELLOW}WARNING: $RUNNING/$CONTAINER_COUNT containers running (>90%)${NC}"
    exit 0
else
    echo -e "${RED}ERROR: Only $RUNNING/$CONTAINER_COUNT containers running (<90%)${NC}"
    exit 1
fi
