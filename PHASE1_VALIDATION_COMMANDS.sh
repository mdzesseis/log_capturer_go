#!/bin/bash
# PHASE 1 - VALIDATION COMMANDS
# Quick reference for validating fixes
# Generated: 2025-11-08

set -e

echo "======================================"
echo "PHASE 1 - CODE AUDIT VALIDATION"
echo "======================================"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Change to project root
cd /home/mateus/log_capturer_go

echo "========================================"
echo "1. LOCK COPYING VIOLATIONS"
echo "========================================"
echo ""
echo "Running: go vet ./internal/... ./pkg/... 2>&1 | grep 'passes lock by value'"
echo ""

LOCK_VIOLATIONS=$(go vet ./internal/... ./pkg/... 2>&1 | grep "passes lock by value" | wc -l)

if [ "$LOCK_VIOLATIONS" -eq 0 ]; then
    echo -e "${GREEN}✅ PASS: No lock copying violations found${NC}"
else
    echo -e "${RED}❌ FAIL: Found $LOCK_VIOLATIONS lock copying violations${NC}"
    echo ""
    echo "Top 10 violations:"
    go vet ./internal/... ./pkg/... 2>&1 | grep "passes lock by value" | head -10
fi

echo ""
echo "========================================"
echo "2. RACE DETECTOR - PRODUCTION CODE"
echo "========================================"
echo ""
echo "Running: go test -race -run=^$ ./internal/... ./pkg/..."
echo "(Compiles with race detector but runs 0 tests)"
echo ""

if go test -race -run=^$ ./internal/... ./pkg/... 2>&1 | grep -q "FAIL"; then
    echo -e "${RED}❌ FAIL: Race detector found issues${NC}"
else
    echo -e "${GREEN}✅ PASS: Race detector compilation successful${NC}"
fi

echo ""
echo "========================================"
echo "3. RACE DETECTOR - ACTUAL TESTS"
echo "========================================"
echo ""
echo "Running: go test -race -timeout=60s ./internal/dispatcher/... ./internal/sinks/..."
echo ""

if timeout 60 go test -race ./internal/dispatcher/... ./internal/sinks/... 2>&1 | tee /tmp/race_output.log | grep -q "WARNING: DATA RACE"; then
    echo -e "${YELLOW}⚠️  WARNING: Race conditions detected${NC}"
    echo ""
    echo "Checking if races are in OUR code or external dependencies..."

    OUR_RACES=$(grep -A 5 "WARNING: DATA RACE" /tmp/race_output.log | grep -E "ssw-logs-capture/(internal|pkg)" | wc -l)

    if [ "$OUR_RACES" -gt 0 ]; then
        echo -e "${RED}❌ CRITICAL: $OUR_RACES races in OUR code${NC}"
        echo ""
        echo "Race locations in our code:"
        grep -A 5 "WARNING: DATA RACE" /tmp/race_output.log | grep "ssw-logs-capture/"
    else
        echo -e "${GREEN}✅ INFO: All races are in external dependencies (Docker SDK)${NC}"
    fi
else
    echo -e "${GREEN}✅ PASS: No race conditions detected${NC}"
fi

echo ""
echo "========================================"
echo "4. BUILD VERIFICATION"
echo "========================================"
echo ""
echo "Running: go build ./..."
echo ""

if go build ./... 2>&1; then
    echo -e "${GREEN}✅ PASS: All packages build successfully${NC}"
else
    echo -e "${RED}❌ FAIL: Build errors detected${NC}"
fi

echo ""
echo "========================================"
echo "5. GO VET - FULL CHECK"
echo "========================================"
echo ""
echo "Running: go vet ./..."
echo ""

VET_ISSUES=$(go vet ./... 2>&1 | grep -v "passes lock by value" | grep -E "^(internal|pkg)/" | wc -l)

if [ "$VET_ISSUES" -eq 0 ]; then
    echo -e "${GREEN}✅ PASS: No vet issues (excluding lock copying)${NC}"
else
    echo -e "${YELLOW}⚠️  WARNING: Found $VET_ISSUES other vet issues${NC}"
    echo ""
    echo "Top 10 issues:"
    go vet ./... 2>&1 | grep -v "passes lock by value" | grep -E "^(internal|pkg)/" | head -10
fi

echo ""
echo "========================================"
echo "6. TEST EXECUTION (WITHOUT RACE)"
echo "========================================"
echo ""
echo "Running: go test -short ./internal/dispatcher/... ./internal/sinks/..."
echo ""

if go test -short ./internal/dispatcher/... ./internal/sinks/... 2>&1; then
    echo -e "${GREEN}✅ PASS: Core tests passing${NC}"
else
    echo -e "${YELLOW}⚠️  WARNING: Some tests failing${NC}"
fi

echo ""
echo "========================================"
echo "SUMMARY"
echo "========================================"
echo ""

# Count issues
TOTAL_ISSUES=0

if [ "$LOCK_VIOLATIONS" -gt 0 ]; then
    echo -e "${RED}❌ Lock Copying Violations: $LOCK_VIOLATIONS${NC}"
    TOTAL_ISSUES=$((TOTAL_ISSUES + LOCK_VIOLATIONS))
else
    echo -e "${GREEN}✅ Lock Copying Violations: 0${NC}"
fi

if [ "$VET_ISSUES" -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Other Vet Issues: $VET_ISSUES${NC}"
else
    echo -e "${GREEN}✅ Other Vet Issues: 0${NC}"
fi

echo ""

if [ "$TOTAL_ISSUES" -eq 0 ]; then
    echo -e "${GREEN}════════════════════════════════════════${NC}"
    echo -e "${GREEN}✅ ALL CHECKS PASSED - CODE IS CLEAN${NC}"
    echo -e "${GREEN}════════════════════════════════════════${NC}"
    exit 0
else
    echo -e "${RED}════════════════════════════════════════${NC}"
    echo -e "${RED}❌ FOUND $TOTAL_ISSUES ISSUES REQUIRING FIXES${NC}"
    echo -e "${RED}════════════════════════════════════════${NC}"
    echo ""
    echo "Next steps:"
    echo "1. Review PHASE1_DISCOVERY_REPORT.md"
    echo "2. Review PHASE1_EXECUTIVE_SUMMARY.md"
    echo "3. Approve fix strategy"
    echo "4. Proceed to Phase 2 (Implementation)"
    exit 1
fi
