#!/bin/bash

# Test Runner Script para SSW Logs Capture
# Executa testes unitários e de integração com cobertura completa

set -e

# Cores para output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Função para logging
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Configurações
TEST_TIMEOUT=${TEST_TIMEOUT:-10m}
COVERAGE_THRESHOLD=${COVERAGE_THRESHOLD:-94}
TEST_RESULTS_DIR="/app/test-results"
COVERAGE_DIR="/app/coverage"
TEST_DATA_DIR="/app/test-data"

# Função para setup inicial
setup_test_environment() {
    log_info "Setting up test environment..."

    # Criar diretórios necessários
    mkdir -p "$TEST_RESULTS_DIR" "$COVERAGE_DIR" "$TEST_DATA_DIR"
    mkdir -p /app/logs /app/positions /app/dlq /app/batch_persistence

    # Criar arquivos de teste
    echo "test log line 1" > "$TEST_DATA_DIR/test.log"
    echo "test log line 2" >> "$TEST_DATA_DIR/test.log"
    echo "test log line 3" >> "$TEST_DATA_DIR/test.log"

    # Configurar permissões
    chmod -R 755 /app/test-data

    log_success "Test environment setup completed"
}

# Função para verificar dependências
check_dependencies() {
    log_info "Checking dependencies..."

    local deps=("go" "gocov" "gocov-xml" "gocov-html" "golangci-lint")
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            log_error "Dependency not found: $dep"
            exit 1
        fi
    done

    log_success "All dependencies are available"
}

# Função para executar linting
run_linting() {
    log_info "Running code linting..."

    if golangci-lint run --out-format checkstyle > "$TEST_RESULTS_DIR/golangci-lint-report.xml" 2>&1; then
        log_success "Linting passed"
    else
        log_warning "Linting found issues (see report)"
    fi
}

# Função para executar testes unitários
run_unit_tests() {
    log_info "Running unit tests..."

    # Executar testes com cobertura
    go test -v \
        -timeout="$TEST_TIMEOUT" \
        -coverprofile="$COVERAGE_DIR/unit-coverage.out" \
        -covermode=atomic \
        -json \
        ./... > "$TEST_RESULTS_DIR/unit-tests.json" 2>&1

    local exit_code=$?

    # Converter para formato JUnit XML
    if command -v go-junit-report &> /dev/null; then
        cat "$TEST_RESULTS_DIR/unit-tests.json" | go-junit-report > "$TEST_RESULTS_DIR/unit-tests.xml"
    fi

    if [ $exit_code -eq 0 ]; then
        log_success "Unit tests passed"
    else
        log_error "Unit tests failed"
        return $exit_code
    fi
}

# Função para executar testes de integração
run_integration_tests() {
    log_info "Running integration tests..."

    # Aguardar serviços dependentes estarem prontos
    wait_for_services

    # Executar testes de integração com tags específicas
    go test -v \
        -timeout="$TEST_TIMEOUT" \
        -tags=integration \
        -coverprofile="$COVERAGE_DIR/integration-coverage.out" \
        -covermode=atomic \
        -json \
        ./tests/integration/... > "$TEST_RESULTS_DIR/integration-tests.json" 2>&1

    local exit_code=$?

    # Converter para formato JUnit XML
    if command -v go-junit-report &> /dev/null; then
        cat "$TEST_RESULTS_DIR/integration-tests.json" | go-junit-report > "$TEST_RESULTS_DIR/integration-tests.xml"
    fi

    if [ $exit_code -eq 0 ]; then
        log_success "Integration tests passed"
    else
        log_error "Integration tests failed"
        return $exit_code
    fi
}

# Função para aguardar serviços
wait_for_services() {
    log_info "Waiting for dependent services..."

    # Aguardar Loki se configurado
    if [ -n "$LOKI_URL" ]; then
        log_info "Waiting for Loki at $LOKI_URL"
        timeout 60 bash -c 'until curl -s '"$LOKI_URL"'/ready; do sleep 2; done'
    fi

    # Aguardar Docker socket se disponível
    if [ -S /var/run/docker.sock ]; then
        log_info "Docker socket available"
    fi

    log_success "Services are ready"
}

# Função para gerar relatório de cobertura
generate_coverage_report() {
    log_info "Generating coverage report..."

    # Merge coverage files
    if [ -f "$COVERAGE_DIR/unit-coverage.out" ] && [ -f "$COVERAGE_DIR/integration-coverage.out" ]; then
        gocovmerge "$COVERAGE_DIR/unit-coverage.out" "$COVERAGE_DIR/integration-coverage.out" > "$COVERAGE_DIR/total-coverage.out"
    elif [ -f "$COVERAGE_DIR/unit-coverage.out" ]; then
        cp "$COVERAGE_DIR/unit-coverage.out" "$COVERAGE_DIR/total-coverage.out"
    elif [ -f "$COVERAGE_DIR/integration-coverage.out" ]; then
        cp "$COVERAGE_DIR/integration-coverage.out" "$COVERAGE_DIR/total-coverage.out"
    else
        log_error "No coverage files found"
        return 1
    fi

    # Gerar relatório HTML
    gocov convert "$COVERAGE_DIR/total-coverage.out" | gocov-html > "$COVERAGE_DIR/coverage.html"

    # Gerar relatório XML (Cobertura format)
    gocov convert "$COVERAGE_DIR/total-coverage.out" | gocov-xml > "$COVERAGE_DIR/coverage.xml"

    # Calcular percentual de cobertura
    local coverage_percent=$(go tool cover -func="$COVERAGE_DIR/total-coverage.out" | grep total | awk '{print $3}' | sed 's/%//')

    echo "$coverage_percent" > "$COVERAGE_DIR/coverage-percent.txt"

    log_info "Coverage: ${coverage_percent}%"

    # Verificar threshold
    if (( $(echo "$coverage_percent >= $COVERAGE_THRESHOLD" | bc -l) )); then
        log_success "Coverage threshold met: ${coverage_percent}% >= ${COVERAGE_THRESHOLD}%"
        return 0
    else
        log_error "Coverage threshold not met: ${coverage_percent}% < ${COVERAGE_THRESHOLD}%"
        return 1
    fi
}

# Função para executar testes de performance
run_performance_tests() {
    log_info "Running performance tests..."

    go test -v \
        -timeout="$TEST_TIMEOUT" \
        -bench=. \
        -benchmem \
        -cpuprofile="$TEST_RESULTS_DIR/cpu.prof" \
        -memprofile="$TEST_RESULTS_DIR/mem.prof" \
        -json \
        ./tests/performance/... > "$TEST_RESULTS_DIR/performance-tests.json" 2>&1

    if [ $? -eq 0 ]; then
        log_success "Performance tests completed"
    else
        log_warning "Performance tests had issues"
    fi
}

# Função para executar testes de stress
run_stress_tests() {
    log_info "Running stress tests..."

    go test -v \
        -timeout=30m \
        -tags=stress \
        -json \
        ./tests/stress/... > "$TEST_RESULTS_DIR/stress-tests.json" 2>&1

    if [ $? -eq 0 ]; then
        log_success "Stress tests completed"
    else
        log_warning "Stress tests had issues"
    fi
}

# Função para gerar relatório final
generate_final_report() {
    log_info "Generating final test report..."

    cat > "$TEST_RESULTS_DIR/test-summary.json" << EOF
{
    "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "test_suite": "SSW Logs Capture Go",
    "coverage_threshold": $COVERAGE_THRESHOLD,
    "coverage_actual": $(cat "$COVERAGE_DIR/coverage-percent.txt" 2>/dev/null || echo "0"),
    "tests_executed": {
        "unit": "$([ -f "$TEST_RESULTS_DIR/unit-tests.json" ] && echo "true" || echo "false")",
        "integration": "$([ -f "$TEST_RESULTS_DIR/integration-tests.json" ] && echo "true" || echo "false")",
        "performance": "$([ -f "$TEST_RESULTS_DIR/performance-tests.json" ] && echo "true" || echo "false")",
        "stress": "$([ -f "$TEST_RESULTS_DIR/stress-tests.json" ] && echo "true" || echo "false")"
    },
    "reports_generated": {
        "coverage_html": "$([ -f "$COVERAGE_DIR/coverage.html" ] && echo "coverage/coverage.html" || echo "null")",
        "coverage_xml": "$([ -f "$COVERAGE_DIR/coverage.xml" ] && echo "coverage/coverage.xml" || echo "null")",
        "lint_report": "$([ -f "$TEST_RESULTS_DIR/golangci-lint-report.xml" ] && echo "test-results/golangci-lint-report.xml" || echo "null")"
    }
}
EOF

    log_success "Final report generated"
}

# Função para iniciar health server
start_health_server() {
    log_info "Starting health server on :9090"

    # Criar script simples de health check
    cat > /tmp/health-server.go << 'EOF'
package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "time"
)

type HealthStatus struct {
    Status    string    `json:"status"`
    Timestamp time.Time `json:"timestamp"`
    Uptime    string    `json:"uptime"`
    TestsDir  string    `json:"tests_dir"`
}

var startTime = time.Now()

func healthHandler(w http.ResponseWriter, r *http.Request) {
    status := HealthStatus{
        Status:    "healthy",
        Timestamp: time.Now(),
        Uptime:    time.Since(startTime).String(),
        TestsDir:  "/app/test-results",
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(status)
}

func main() {
    http.HandleFunc("/health", healthHandler)
    http.HandleFunc("/", healthHandler)

    fmt.Println("Health server starting on :9090")
    if err := http.ListenAndServe(":9090", nil); err != nil {
        fmt.Printf("Health server error: %v\n", err)
        os.Exit(1)
    }
}
EOF

    go run /tmp/health-server.go &
    echo $! > /tmp/health-server.pid
}

# Função para cleanup
cleanup() {
    log_info "Cleaning up..."

    if [ -f /tmp/health-server.pid ]; then
        kill $(cat /tmp/health-server.pid) 2>/dev/null || true
        rm -f /tmp/health-server.pid
    fi
}

# Trap para cleanup
trap cleanup EXIT

# Função principal
main() {
    log_info "Starting SSW Logs Capture Test Suite"
    echo "============================================"

    # Iniciar health server em background
    start_health_server

    # Setup
    setup_test_environment
    check_dependencies

    # Executar diferentes tipos de teste baseado em parâmetros
    case "${1:-all}" in
        "unit")
            run_linting
            run_unit_tests
            ;;
        "integration")
            run_integration_tests
            ;;
        "performance")
            run_performance_tests
            ;;
        "stress")
            run_stress_tests
            ;;
        "coverage")
            run_unit_tests
            run_integration_tests
            generate_coverage_report
            ;;
        "all")
            run_linting
            run_unit_tests
            run_integration_tests
            generate_coverage_report
            run_performance_tests
            ;;
        *)
            log_error "Unknown test type: $1"
            echo "Usage: $0 [unit|integration|performance|stress|coverage|all]"
            exit 1
            ;;
    esac

    # Gerar relatório final
    generate_final_report

    log_success "Test suite completed successfully"
    echo "============================================"
    echo "Test results available in: $TEST_RESULTS_DIR"
    echo "Coverage reports available in: $COVERAGE_DIR"
    echo "Health endpoint: http://localhost:9090/health"

    # Manter container ativo se em modo daemon
    if [ "${DAEMON_MODE:-false}" = "true" ]; then
        log_info "Running in daemon mode, keeping container alive..."
        tail -f /dev/null
    fi
}

# Executar função principal com argumentos
main "$@"