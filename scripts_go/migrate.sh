#!/bin/bash

# Script de Migração Python → Go
# SSW Logs Capture Migration Automation

set -e

# Cores para output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Função para print colorido
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Verificar se estamos no diretório correto
if [ ! -f "go.mod" ]; then
    print_error "Este script deve ser executado no diretório refatoramento_GO"
    exit 1
fi

# Função para mostrar ajuda
show_help() {
    echo "SSW Logs Capture Migration Script"
    echo ""
    echo "Uso: $0 [comando]"
    echo ""
    echo "Comandos disponíveis:"
    echo "  validate       - Validar ambas as versões"
    echo "  backup         - Fazer backup das configurações"
    echo "  cutover        - Executar migração completa"
    echo "  rollback       - Reverter para versão Python"
    echo "  status         - Verificar status das versões"
    echo "  compare        - Comparar performance"
    echo ""
}

# Função para validar versões
validate() {
    print_status "Validando versões Python e Go..."

    # Testar Python
    python_health=$(curl -s http://localhost:8401/health 2>/dev/null || echo "error")
    if [[ $python_health == *"healthy"* ]]; then
        print_success "Python version: Healthy"
    else
        print_warning "Python version: Not responding"
    fi

    # Testar Go
    go_health=$(curl -s http://localhost:8402/health 2>/dev/null || echo "error")
    if [[ $go_health == *"healthy"* ]]; then
        print_success "Go version: Healthy"
    else
        print_error "Go version: Not responding"
        exit 1
    fi

    print_success "Validação concluída"
}

# Função para backup
backup() {
    print_status "Criando backup das configurações..."

    backup_dir="backup_$(date +%Y%m%d_%H%M%S)"
    mkdir -p "$backup_dir"

    # Backup de configurações
    cp -r ../docker-compose.yml "$backup_dir/" 2>/dev/null || true
    cp -r ../.env "$backup_dir/" 2>/dev/null || true

    # Backup de dados de posição
    docker cp log_capturer:/app/data/positions "$backup_dir/" 2>/dev/null || true

    print_success "Backup criado em: $backup_dir"
}

# Função para cutover
cutover() {
    print_status "Iniciando migração completa Python → Go..."

    # Validar primeiro
    validate

    # Fazer backup
    backup

    print_status "Parando versão Python..."
    docker stop log_capturer 2>/dev/null || true

    print_status "Reconfigurando versão Go para portas de produção..."

    # Parar Go atual
    docker-compose down

    # Atualizar portas no docker-compose
    sed -i 's/8402:8401/8401:8401/g' docker-compose.yml
    sed -i 's/8002:8001/8001:8001/g' docker-compose.yml
    sed -i 's/3101:3100/3100:3100/g' docker-compose.yml

    # Subir versão Go nas portas de produção
    docker-compose up -d log_capturer_go loki

    print_status "Aguardando inicialização..."
    sleep 10

    # Validar nova configuração
    go_health=$(curl -s http://localhost:8401/health 2>/dev/null || echo "error")
    if [[ $go_health == *"healthy"* ]]; then
        print_success "Migração concluída com sucesso!"
        print_success "Go version rodando em portas de produção"
        print_status "APIs disponíveis:"
        print_status "  - Health: http://localhost:8401/health"
        print_status "  - Metrics: http://localhost:8001/metrics"
    else
        print_error "Falha na migração. Executando rollback automático..."
        rollback
        exit 1
    fi
}

# Função para rollback
rollback() {
    print_warning "Executando rollback para versão Python..."

    # Parar versão Go
    docker-compose down 2>/dev/null || true

    # Restaurar portas originais
    sed -i 's/8401:8401/8402:8401/g' docker-compose.yml
    sed -i 's/8001:8001/8002:8001/g' docker-compose.yml
    sed -i 's/3100:3100/3101:3100/g' docker-compose.yml

    # Subir Go nas portas alternativas
    docker-compose up -d log_capturer_go loki

    # Iniciar Python
    docker start log_capturer 2>/dev/null || true

    print_warning "Rollback concluído. Versão Python restaurada."
}

# Função para status
status() {
    print_status "Status das versões:"
    echo ""

    # Python
    python_status=$(docker ps --filter "name=log_capturer" --format "{{.Status}}" 2>/dev/null || echo "Not running")
    python_health=$(curl -s http://localhost:8401/health 2>/dev/null || echo '{"status":"unreachable"}')
    echo "🐍 Python:"
    echo "   Status: $python_status"
    echo "   Health: $python_health"
    echo ""

    # Go
    go_status=$(docker ps --filter "name=ssw-logs-capture-go" --format "{{.Status}}" 2>/dev/null || echo "Not running")
    go_health=$(curl -s http://localhost:8402/health 2>/dev/null || echo '{"status":"unreachable"}')
    echo "🔥 Go:"
    echo "   Status: $go_status"
    echo "   Health: $go_health"
}

# Função para comparar performance
compare() {
    print_status "Comparando performance das versões:"
    echo ""

    # Stats Python
    echo "🐍 Python Resource Usage:"
    docker stats log_capturer --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}" 2>/dev/null || echo "Container não está rodando"
    echo ""

    # Stats Go
    echo "🔥 Go Resource Usage:"
    docker stats ssw-logs-capture-go --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}" 2>/dev/null || echo "Container não está rodando"
    echo ""

    # Métricas de logs
    echo "📊 Logs Processados:"
    go_logs=$(curl -s http://localhost:8002/metrics 2>/dev/null | grep "logs_processed_total" | grep -o "} [0-9]*" | sed 's/} //' | awk '{sum += $1} END {print sum}' || echo "0")
    echo "🔥 Go: $go_logs logs processados"
}

# Processar argumentos
case "${1:-help}" in
    "validate")
        validate
        ;;
    "backup")
        backup
        ;;
    "cutover")
        cutover
        ;;
    "rollback")
        rollback
        ;;
    "status")
        status
        ;;
    "compare")
        compare
        ;;
    "help"|*)
        show_help
        ;;
esac