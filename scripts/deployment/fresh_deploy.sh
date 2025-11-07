#!/bin/bash
set -e

echo "========================================="
echo "=== Fresh Docker Deployment ==="
echo "========================================="
echo ""

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 1. Stop all containers
echo "1. Stopping containers..."
docker-compose down -v 2>/dev/null || true
echo -e "${GREEN}✅ Containers stopped${NC}"
echo ""

# 2. Remove log_capturer_go volumes
echo "2. Removing volumes..."
docker volume ls -q | grep -i log_capturer_go | xargs -r docker volume rm 2>/dev/null || true
docker volume ls -q | grep -i ssw | xargs -r docker volume rm 2>/dev/null || true
echo -e "${GREEN}✅ Volumes removed${NC}"
echo ""

# 3. Clean up old logs/data
echo "3. Cleaning up old data..."
rm -rf data/positions/*.json 2>/dev/null || true
rm -rf data/checkpoints/* 2>/dev/null || true
rm -rf data/dlq/* 2>/dev/null || true
rm -rf /tmp/logs/output/* 2>/dev/null || true
echo -e "${GREEN}✅ Old data cleaned${NC}"
echo ""

# 4. Optional: rebuild without cache
echo "4. Docker image build..."
read -p "Rebuild images without cache? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "Rebuilding images..."
    docker-compose build --no-cache log_capturer_go
else
    echo "Using cached images..."
    docker-compose build log_capturer_go
fi
echo -e "${GREEN}✅ Images ready${NC}"
echo ""

# 5. Start all services
echo "5. Starting services..."
docker-compose up -d
echo -e "${GREEN}✅ Services starting${NC}"
echo ""

# 6. Wait for services to be ready
echo "6. Waiting for services to start..."
sleep 10

# Show running containers
echo ""
echo "Running containers:"
docker-compose ps
echo ""

# 7. Wait for log_capturer_go to be ready
echo "7. Waiting for log_capturer_go to be ready..."
MAX_RETRIES=30
RETRY_COUNT=0

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if curl -sf http://localhost:8401/health > /dev/null 2>&1; then
        echo -e "${GREEN}✅ Log capturer is ready!${NC}"
        break
    fi
    echo -n "."
    sleep 2
    RETRY_COUNT=$((RETRY_COUNT + 1))
done

if [ $RETRY_COUNT -eq $MAX_RETRIES ]; then
    echo -e "${YELLOW}⚠️  Timeout waiting for log_capturer_go${NC}"
    echo "Check logs: docker-compose logs log_capturer_go"
    exit 1
fi

echo ""
echo ""
echo "========================================="
echo "=== ✅ Deployment Complete ==="
echo "========================================="
echo ""
echo "Services:"
echo "  - Log Capturer: http://localhost:8401"
echo "  - Metrics:      http://localhost:8001/metrics"
echo "  - Grafana:      http://localhost:3000 (admin/admin)"
echo "  - Prometheus:   http://localhost:9090"
echo "  - Jaeger:       http://localhost:16686"
echo "  - Loki:         http://localhost:3100"
echo ""
echo "Quick checks:"
echo "  curl http://localhost:8401/health"
echo "  curl http://localhost:8001/metrics | grep log_capturer_go"
echo "  docker-compose logs -f log_capturer_go"
echo ""
