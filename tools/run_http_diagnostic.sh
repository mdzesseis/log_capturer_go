#!/bin/bash
#
# HTTP Transport Diagnostic Runner
#
# This script builds and runs the HTTP transport diagnostic tool to verify
# MaxConnsPerHost configuration and detect potential goroutine leaks.
#

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Project root
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo -e "${BLUE}=== HTTP Transport Configuration Diagnostic ===${NC}"
echo ""

# Check Go version
echo -e "${YELLOW}Checking Go version...${NC}"
GO_VERSION=$(go version | awk '{print $3}')
echo "Go version: $GO_VERSION"
echo ""

# Minimum Go version check (1.11 for MaxConnsPerHost)
MIN_VERSION="go1.11"
if [[ "$GO_VERSION" < "$MIN_VERSION" ]]; then
    echo -e "${RED}WARNING: MaxConnsPerHost requires Go 1.11+${NC}"
    echo "Your version: $GO_VERSION"
    echo "This may affect diagnostic results"
    echo ""
fi

# Build diagnostic tool
echo -e "${YELLOW}Building diagnostic tool...${NC}"
cd "$PROJECT_ROOT"
go build -o bin/http_transport_diagnostic tools/http_transport_diagnostic.go
echo -e "${GREEN}✓ Build successful${NC}"
echo ""

# Create reports directory
mkdir -p "$PROJECT_ROOT/reports"

# Run diagnostic
echo -e "${YELLOW}Running diagnostic...${NC}"
echo ""

OUTPUT_FILE="$PROJECT_ROOT/reports/http_transport_diagnostic_$(date +%Y%m%d_%H%M%S).json"

# Run and capture output
if ./bin/http_transport_diagnostic > "$OUTPUT_FILE"; then
    echo -e "${GREEN}✓ Diagnostic completed successfully${NC}"
else
    echo -e "${RED}✗ Diagnostic completed with issues${NC}"
fi

echo ""
echo -e "${BLUE}Report saved to: $OUTPUT_FILE${NC}"
echo ""

# Parse and display summary
echo -e "${YELLOW}=== Diagnostic Summary ===${NC}"
echo ""

# Extract key results using jq if available
if command -v jq &> /dev/null; then
    OVERALL_STATUS=$(jq -r '.overall_status' "$OUTPUT_FILE")
    SUMMARY=$(jq -r '.summary' "$OUTPUT_FILE")

    if [ "$OVERALL_STATUS" = "PASS" ]; then
        echo -e "${GREEN}Overall Status: $OVERALL_STATUS${NC}"
    elif [ "$OVERALL_STATUS" = "WARN" ]; then
        echo -e "${YELLOW}Overall Status: $OVERALL_STATUS${NC}"
    else
        echo -e "${RED}Overall Status: $OVERALL_STATUS${NC}"
    fi

    echo "Summary: $SUMMARY"
    echo ""

    # Show test results
    echo "Test Results:"
    jq -r '.results[] | "[\(.status)] \(.test_name)"' "$OUTPUT_FILE" | while read -r line; do
        if [[ "$line" =~ ^\[PASS\] ]]; then
            echo -e "${GREEN}$line${NC}"
        elif [[ "$line" =~ ^\[WARN\] ]]; then
            echo -e "${YELLOW}$line${NC}"
        elif [[ "$line" =~ ^\[FAIL\] ]]; then
            echo -e "${RED}$line${NC}"
        else
            echo "$line"
        fi
    done
    echo ""

    # Show errors if any
    ERRORS=$(jq -r '.results[] | select(.errors != null and (.errors | length) > 0) | .test_name' "$OUTPUT_FILE")
    if [ -n "$ERRORS" ]; then
        echo -e "${RED}Tests with errors:${NC}"
        jq -r '.results[] | select(.errors != null and (.errors | length) > 0) | "  \(.test_name):\n    \(.errors | join("\n    "))"' "$OUTPUT_FILE"
        echo ""
    fi

    # Show recommendations
    RECOMMENDATIONS=$(jq -r '.results[] | select(.recommendations != null and (.recommendations | length) > 0) | .test_name' "$OUTPUT_FILE")
    if [ -n "$RECOMMENDATIONS" ]; then
        echo -e "${YELLOW}Recommendations:${NC}"
        jq -r '.results[] | select(.recommendations != null and (.recommendations | length) > 0) | "  \(.test_name):\n    \(.recommendations | join("\n    "))"' "$OUTPUT_FILE"
        echo ""
    fi

    # Show key metrics
    echo -e "${BLUE}Key Metrics:${NC}"
    echo "  Go Version: $(jq -r '.go_version' "$OUTPUT_FILE")"
    echo "  Initial Goroutines: $(jq -r '.initial_goroutines' "$OUTPUT_FILE")"

    # Loki configuration
    LOKI_MAX_CONNS=$(jq -r '.results[] | select(.test_name == "Loki Sink HTTP Transport Analysis") | .details.max_conns_per_host_value' "$OUTPUT_FILE")
    if [ "$LOKI_MAX_CONNS" != "null" ]; then
        echo "  Loki MaxConnsPerHost: $LOKI_MAX_CONNS"
    fi

    # Docker configuration
    DOCKER_MAX_CONNS=$(jq -r '.results[] | select(.test_name == "Docker Connection Pool HTTP Transport Analysis") | .details.max_conns_per_host_value' "$OUTPUT_FILE")
    if [ "$DOCKER_MAX_CONNS" != "null" ]; then
        echo "  Docker MaxConnsPerHost: $DOCKER_MAX_CONNS"
    fi

    # Enforcement test results
    MAX_OBSERVED=$(jq -r '.results[] | select(.test_name == "MaxConnsPerHost Enforcement Test") | .details.max_concurrent_connections_observed' "$OUTPUT_FILE")
    if [ "$MAX_OBSERVED" != "null" ]; then
        echo "  Max Concurrent Connections Observed: $MAX_OBSERVED"
    fi

    echo ""
else
    # jq not available, display raw JSON
    echo -e "${YELLOW}(Install 'jq' for formatted output)${NC}"
    echo ""
    cat "$OUTPUT_FILE"
fi

# Display full report location
echo -e "${BLUE}Full report: $OUTPUT_FILE${NC}"
echo ""

# Offer to view detailed report
echo "View detailed report? (y/n)"
read -r VIEW_REPORT
if [ "$VIEW_REPORT" = "y" ] || [ "$VIEW_REPORT" = "Y" ]; then
    if command -v jq &> /dev/null; then
        jq '.' "$OUTPUT_FILE" | less
    else
        less "$OUTPUT_FILE"
    fi
fi

echo ""
echo -e "${BLUE}=== Additional Commands ===${NC}"
echo ""
echo "View Loki Sink configuration:"
echo "  cat internal/sinks/loki_sink.go | grep -A 15 'Transport: &http.Transport'"
echo ""
echo "View Docker Pool configuration:"
echo "  cat pkg/docker/connection_pool.go | grep -A 10 'Transport: &http.Transport'"
echo ""
echo "Monitor goroutines (if app is running):"
echo "  curl http://localhost:6060/debug/pprof/goroutine?debug=2 | grep 'net/http'"
echo ""
echo "Run load test:"
echo "  go test -v ./tests/load -run TestSustainedLoad"
echo ""

exit 0
