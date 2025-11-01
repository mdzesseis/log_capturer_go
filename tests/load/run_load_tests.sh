#!/bin/bash

# Load Testing Script for SSW Logs Capture
# This script facilitates running different load test scenarios

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
API_URL="${API_URL:-http://localhost:8401}"
RESULTS_DIR="./load_test_results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Functions
print_header() {
    echo -e "\n${GREEN}=== $1 ===${NC}\n"
}

print_error() {
    echo -e "${RED}ERROR: $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}WARNING: $1${NC}"
}

print_success() {
    echo -e "${GREEN}SUCCESS: $1${NC}"
}

check_server() {
    print_header "Checking Server Status"

    if curl -s "${API_URL}/health" > /dev/null 2>&1; then
        print_success "Server is running at ${API_URL}"
        return 0
    else
        print_error "Server is not responding at ${API_URL}"
        print_warning "Please start the server first:"
        echo "  docker-compose up -d"
        echo "  OR"
        echo "  go run cmd/main.go"
        return 1
    fi
}

create_results_dir() {
    mkdir -p "${RESULTS_DIR}"
    echo "Results will be saved to: ${RESULTS_DIR}"
}

run_baseline_tests() {
    print_header "Running Baseline Load Tests"

    local log_file="${RESULTS_DIR}/baseline_${TIMESTAMP}.log"

    echo "Running baseline tests (10K, 25K, 50K, 100K logs/sec)..."
    echo "Output: ${log_file}"

    go test -v -run TestLoadBaseline -timeout 30m 2>&1 | tee "${log_file}"

    if [ ${PIPESTATUS[0]} -eq 0 ]; then
        print_success "Baseline tests completed"
        analyze_results "${log_file}"
    else
        print_error "Baseline tests failed"
        return 1
    fi
}

run_quick_sustained() {
    print_header "Running Quick Sustained Test (10 minutes)"

    local log_file="${RESULTS_DIR}/sustained_10min_${TIMESTAMP}.log"

    echo "Running 10-minute sustained test..."
    echo "Output: ${log_file}"

    go test -v -run TestSustainedLoad_10min -timeout 20m 2>&1 | tee "${log_file}"

    if [ ${PIPESTATUS[0]} -eq 0 ]; then
        print_success "10-minute sustained test completed"
        analyze_results "${log_file}"
    else
        print_error "Sustained test failed"
        return 1
    fi
}

run_1h_sustained() {
    print_header "Running 1-Hour Sustained Test"

    local log_file="${RESULTS_DIR}/sustained_1h_${TIMESTAMP}.log"

    echo "Running 1-hour sustained test..."
    echo "Output: ${log_file}"
    echo ""
    print_warning "This test will run for 1 hour. Monitor progress with:"
    echo "  tail -f ${log_file}"
    echo ""

    go test -v -run TestSustainedLoad_1h -timeout 90m 2>&1 | tee "${log_file}"

    if [ ${PIPESTATUS[0]} -eq 0 ]; then
        print_success "1-hour sustained test completed"
        analyze_results "${log_file}"
    else
        print_error "Sustained test failed"
        return 1
    fi
}

run_24h_sustained() {
    print_header "Running 24-Hour Sustained Test"

    local log_file="${RESULTS_DIR}/sustained_24h_${TIMESTAMP}.log"
    local pid_file="${RESULTS_DIR}/sustained_24h.pid"

    echo "Running 24-hour sustained test in background..."
    echo "Output: ${log_file}"
    echo "PID file: ${pid_file}"
    echo ""
    print_warning "This test will run for 24 hours in the background."
    echo ""
    echo "Monitor progress with:"
    echo "  tail -f ${log_file}"
    echo ""
    echo "Stop test with:"
    echo "  kill \$(cat ${pid_file})"
    echo ""

    nohup go test -v -run TestSustainedLoad_24h -timeout 30h > "${log_file}" 2>&1 &
    echo $! > "${pid_file}"

    print_success "24-hour test started (PID: $(cat ${pid_file}))"
    echo "Check status with: ps -p $(cat ${pid_file})"
}

analyze_results() {
    local log_file="$1"

    print_header "Quick Analysis"

    # Extract key metrics
    echo "Throughput:"
    grep "Actual Throughput:" "${log_file}" | tail -1

    echo ""
    echo "Error Rate:"
    grep "Error Rate:" "${log_file}" | tail -1

    echo ""
    echo "Latency:"
    grep "Avg:" "${log_file}" | tail -1

    echo ""
    echo "Memory:"
    grep "Memory Growth:" "${log_file}" | tail -1

    echo ""
    echo "Result:"
    if grep -q "SUSTAINED LOAD TEST PASSED" "${log_file}"; then
        print_success "TEST PASSED ✅"
    elif grep -q "SUCCESS:" "${log_file}"; then
        print_success "TEST PASSED ✅"
    else
        print_error "TEST FAILED ❌"
    fi
}

monitor_running_test() {
    local pid_file="${RESULTS_DIR}/sustained_24h.pid"

    if [ ! -f "${pid_file}" ]; then
        print_error "No running 24h test found"
        return 1
    fi

    local pid=$(cat "${pid_file}")

    if ps -p "${pid}" > /dev/null 2>&1; then
        echo "24-hour test is running (PID: ${pid})"
        echo ""
        echo "Latest output:"
        tail -20 "${RESULTS_DIR}"/sustained_24h_*.log 2>/dev/null || echo "No log file found yet"
    else
        echo "24-hour test is not running (PID ${pid} not found)"
        rm -f "${pid_file}"
    fi
}

collect_system_metrics() {
    print_header "Collecting System Metrics"

    local metrics_file="${RESULTS_DIR}/system_metrics_${TIMESTAMP}.txt"

    {
        echo "=== System Metrics ==="
        echo "Timestamp: $(date)"
        echo ""

        echo "=== CPU and Memory ==="
        top -b -n 1 | head -20
        echo ""

        echo "=== Disk Usage ==="
        df -h
        echo ""

        echo "=== Network Connections ==="
        netstat -an | grep 8401 | head -20
        echo ""

        echo "=== Docker Stats ==="
        docker stats --no-stream 2>/dev/null || echo "Docker not available"
        echo ""

        echo "=== Application Metrics ==="
        curl -s "${API_URL}/metrics" | head -50 || echo "Metrics not available"

    } > "${metrics_file}"

    print_success "System metrics saved to: ${metrics_file}"
}

show_menu() {
    echo ""
    echo "SSW Logs Capture - Load Testing Menu"
    echo "====================================="
    echo ""
    echo "Baseline Tests:"
    echo "  1) Run all baseline tests (10K, 25K, 50K, 100K)"
    echo "  2) Run single baseline test (you choose RPS)"
    echo ""
    echo "Sustained Tests:"
    echo "  3) Quick test (10 minutes)"
    echo "  4) Standard test (1 hour)"
    echo "  5) Full test (24 hours - background)"
    echo ""
    echo "Monitoring:"
    echo "  6) Monitor running 24h test"
    echo "  7) Collect system metrics"
    echo "  8) Analyze latest results"
    echo ""
    echo "  9) Check server status"
    echo "  0) Exit"
    echo ""
}

main() {
    print_header "SSW Logs Capture - Load Testing"

    create_results_dir

    if [ $# -eq 0 ]; then
        # Interactive mode
        while true; do
            show_menu
            read -p "Select option: " choice

            case $choice in
                1)
                    check_server && run_baseline_tests
                    ;;
                2)
                    check_server || continue
                    read -p "Enter target RPS (e.g., 10000): " rps
                    print_header "Running custom baseline test at ${rps} logs/sec"
                    go test -v -run TestLoadBaseline_${rps} -timeout 30m
                    ;;
                3)
                    check_server && run_quick_sustained
                    ;;
                4)
                    check_server && run_1h_sustained
                    ;;
                5)
                    check_server && run_24h_sustained
                    ;;
                6)
                    monitor_running_test
                    ;;
                7)
                    collect_system_metrics
                    ;;
                8)
                    latest_log=$(ls -t ${RESULTS_DIR}/*.log 2>/dev/null | head -1)
                    if [ -n "${latest_log}" ]; then
                        analyze_results "${latest_log}"
                    else
                        print_error "No test results found"
                    fi
                    ;;
                9)
                    check_server
                    ;;
                0)
                    echo "Goodbye!"
                    exit 0
                    ;;
                *)
                    print_error "Invalid option"
                    ;;
            esac

            read -p "Press Enter to continue..."
        done
    else
        # Command-line mode
        case "$1" in
            baseline)
                check_server && run_baseline_tests
                ;;
            quick)
                check_server && run_quick_sustained
                ;;
            1h)
                check_server && run_1h_sustained
                ;;
            24h)
                check_server && run_24h_sustained
                ;;
            monitor)
                monitor_running_test
                ;;
            metrics)
                collect_system_metrics
                ;;
            check)
                check_server
                ;;
            *)
                echo "Usage: $0 [baseline|quick|1h|24h|monitor|metrics|check]"
                echo ""
                echo "Run without arguments for interactive menu"
                exit 1
                ;;
        esac
    fi
}

main "$@"
