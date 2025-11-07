#!/bin/bash
set -e

echo "========================================="
echo "=== Pre-Deployment Checks ==="
echo "========================================="
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

ERRORS=0
WARNINGS=0

# 1. Clean build
echo "1. Building application..."
go clean ./...
if go build -o bin/ssw-logs-capture ./cmd/main.go 2>&1 | tee build.log; then
    echo -e "${GREEN}✅ Build successful${NC}"
else
    echo -e "${RED}❌ Build FAILED${NC}"
    cat build.log
    exit 1
fi
echo ""

# 2. Run all tests
echo "2. Running tests..."
if go test ./... -v -timeout=5m 2>&1 | tee test_results.txt; then
    echo -e "${GREEN}✅ Tests passed${NC}"
else
    echo -e "${RED}❌ Tests FAILED${NC}"
    echo "Check test_results.txt for details"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 3. Race detector on critical packages
echo "3. Running race detector on critical packages..."
RACE_FAILED=0
for pkg in ./internal/dispatcher ./pkg/tracing ./internal/sinks ./internal/app; do
    echo "  Checking $pkg..."
    if go test -race $pkg -timeout=2m 2>&1 | tee -a race_results.txt; then
        echo -e "  ${GREEN}✅ $pkg - race detector clean${NC}"
    else
        echo -e "  ${RED}❌ $pkg - race detector found issues${NC}"
        RACE_FAILED=1
    fi
done

if [ $RACE_FAILED -eq 1 ]; then
    echo -e "${RED}❌ Race detector found issues${NC}"
    echo "Check race_results.txt for details"
    ERRORS=$((ERRORS + 1))
else
    echo -e "${GREEN}✅ Race detector clean${NC}"
fi
echo ""

# 4. Linter (optional - non-fatal)
echo "4. Running linter..."
if command -v golangci-lint &> /dev/null; then
    if golangci-lint run --timeout=5m 2>&1 | tee lint_results.txt; then
        echo -e "${GREEN}✅ Linter passed${NC}"
    else
        echo -e "${YELLOW}⚠️  Linter warnings found (non-fatal)${NC}"
        WARNINGS=$((WARNINGS + 1))
    fi
else
    echo -e "${YELLOW}⚠️  golangci-lint not installed, skipping${NC}"
fi
echo ""

# 5. Config validation
echo "5. Validating configuration files..."
if [ -f configs/config.yaml ]; then
    # Basic YAML syntax check
    if command -v yamllint &> /dev/null; then
        if yamllint configs/config.yaml 2>&1; then
            echo -e "${GREEN}✅ config.yaml syntax valid${NC}"
        else
            echo -e "${YELLOW}⚠️  config.yaml has warnings${NC}"
            WARNINGS=$((WARNINGS + 1))
        fi
    else
        echo -e "${YELLOW}⚠️  yamllint not installed, skipping YAML validation${NC}"
    fi

    # Check for placeholder values
    if grep -q "YOUR_" configs/config.yaml; then
        echo -e "${YELLOW}⚠️  Config contains placeholder values (YOUR_*)${NC}"
        WARNINGS=$((WARNINGS + 1))
    fi

    echo -e "${GREEN}✅ Config files present${NC}"
else
    echo -e "${RED}❌ config.yaml not found${NC}"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 6. Docker build test
echo "6. Testing Docker build..."
if docker-compose build log_capturer 2>&1 | tee docker_build.log; then
    echo -e "${GREEN}✅ Docker build successful${NC}"
else
    echo -e "${RED}❌ Docker build FAILED${NC}"
    tail -50 docker_build.log
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 7. Check for common issues
echo "7. Checking for common issues..."

# Check for io/ioutil usage (deprecated)
if grep -r "io/ioutil" --include="*.go" . 2>/dev/null | grep -v vendor | grep -v ".git"; then
    echo -e "${YELLOW}⚠️  Deprecated io/ioutil usage found${NC}"
    WARNINGS=$((WARNINGS + 1))
else
    echo -e "${GREEN}✅ No deprecated io/ioutil usage${NC}"
fi

# Check for TODO comments in critical sections
TODO_COUNT=$(grep -r "TODO" --include="*.go" internal/dispatcher internal/sinks pkg/tracing 2>/dev/null | grep -v vendor | wc -l)
if [ $TODO_COUNT -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Found $TODO_COUNT TODO comments in critical packages${NC}"
    WARNINGS=$((WARNINGS + 1))
else
    echo -e "${GREEN}✅ No TODO comments in critical packages${NC}"
fi

echo ""

# 8. Check dependencies
echo "8. Checking dependencies..."
if go mod verify; then
    echo -e "${GREEN}✅ Go modules verified${NC}"
else
    echo -e "${RED}❌ Go modules verification failed${NC}"
    ERRORS=$((ERRORS + 1))
fi

if go mod tidy -diff 2>&1 | tee mod_tidy.log; then
    echo -e "${GREEN}✅ Go modules are tidy${NC}"
else
    echo -e "${YELLOW}⚠️  Go modules need tidying${NC}"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# Summary
echo "========================================="
echo "=== Pre-Deployment Check Summary ==="
echo "========================================="
echo ""

if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    echo -e "${GREEN}✅✅✅ ALL CHECKS PASSED ✅✅✅${NC}"
    echo ""
    echo "Ready for deployment!"
    exit 0
elif [ $ERRORS -eq 0 ]; then
    echo -e "${YELLOW}✅ CHECKS PASSED WITH WARNINGS ⚠️${NC}"
    echo ""
    echo "Warnings: $WARNINGS"
    echo "You can proceed with deployment, but review warnings."
    exit 0
else
    echo -e "${RED}❌ CHECKS FAILED ❌${NC}"
    echo ""
    echo "Errors: $ERRORS"
    echo "Warnings: $WARNINGS"
    echo ""
    echo "Fix errors before deploying!"
    exit 1
fi
