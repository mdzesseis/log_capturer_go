#!/bin/bash

# Quick Fix Verification Script
# Verifies the goroutine leak fix was applied correctly

set -e

echo "========================================"
echo "GOROUTINE LEAK FIX VERIFICATION"
echo "========================================"
echo ""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

ERRORS=0

echo "1. Checking if readerWg field was added..."
if grep -q "readerWg.*sync.WaitGroup" internal/monitors/container_monitor.go; then
    echo -e "${GREEN}✅ PASS: readerWg field found${NC}"
else
    echo -e "${RED}❌ FAIL: readerWg field not found${NC}"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "2. Checking if rotation Wait() uses readerWg..."
if grep -A2 -B2 "Wait for reader goroutine" internal/monitors/container_monitor.go | grep -q "mc.readerWg.Wait()"; then
    echo -e "${GREEN}✅ PASS: Rotation synchronization uses readerWg${NC}"
else
    echo -e "${RED}❌ FAIL: Rotation synchronization not fixed${NC}"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "3. Checking if reader goroutine uses readerWg..."
if grep -A5 "Goroutine para ler do stream" internal/monitors/container_monitor.go | grep -q "mc.readerWg.Add(1)"; then
    echo -e "${GREEN}✅ PASS: Reader goroutine tracked with readerWg${NC}"
else
    echo -e "${RED}❌ FAIL: Reader goroutine not tracked correctly${NC}"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "4. Checking if reader cleanup uses readerWg..."
if grep -A7 "Goroutine para ler do stream" internal/monitors/container_monitor.go | grep -q "mc.readerWg.Done()"; then
    echo -e "${GREEN}✅ PASS: Reader cleanup uses readerWg${NC}"
else
    echo -e "${RED}❌ FAIL: Reader cleanup not fixed${NC}"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "5. Checking if heartbeatWg is still used for heartbeat..."
if grep -A10 "Enviar heartbeat" internal/monitors/container_monitor.go | grep -q "mc.heartbeatWg.Add(1)"; then
    echo -e "${GREEN}✅ PASS: Heartbeat still uses heartbeatWg${NC}"
else
    echo -e "${RED}❌ FAIL: Heartbeat tracking changed incorrectly${NC}"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "6. Building project..."
if go build -o bin/log_capturer cmd/main.go 2>&1; then
    echo -e "${GREEN}✅ PASS: Build successful${NC}"
else
    echo -e "${RED}❌ FAIL: Build failed${NC}"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "7. Compiling tests..."
if go test -c ./internal/monitors -o /tmp/monitors_test 2>&1 >/dev/null; then
    echo -e "${GREEN}✅ PASS: Tests compile successfully${NC}"
else
    echo -e "${RED}❌ FAIL: Tests failed to compile${NC}"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "========================================"
echo "VERIFICATION SUMMARY"
echo "========================================"
echo ""

if [ $ERRORS -eq 0 ]; then
    echo -e "${GREEN}✅✅✅ ALL CHECKS PASSED ✅✅✅${NC}"
    echo ""
    echo "The goroutine leak fix has been correctly applied."
    echo ""
    echo "Next steps:"
    echo "  1. Run full test suite: go test -race ./..."
    echo "  2. Run validation script: ./tests/goroutine_leak_fix_validation.sh"
    echo "  3. Deploy to staging environment"
    echo ""
    exit 0
else
    echo -e "${RED}❌ $ERRORS CHECK(S) FAILED ❌${NC}"
    echo ""
    echo "Please review the fix implementation."
    echo ""
    exit 1
fi
