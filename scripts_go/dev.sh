#!/bin/bash

# Script de desenvolvimento para SSW Logs Capture Go

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
    echo "SSW Logs Capture Go - Script de Desenvolvimento"
    echo ""
    echo "Uso: $0 [comando]"
    echo ""
    echo "Comandos disponíveis:"
    echo "  build          - Compilar a aplicação"
    echo "  run            - Executar localmente"
    echo "  test           - Executar testes"
    echo "  docker-build   - Build da imagem Docker"
    echo "  docker-run     - Executar com Docker Compose"
    echo "  docker-stop    - Parar containers Docker"
    echo "  docker-logs    - Ver logs dos containers"
    echo "  clean          - Limpar arquivos temporários"
    echo "  deps           - Atualizar dependências"
    echo "  fmt            - Formatar código"
    echo "  lint           - Executar linter"
    echo "  health         - Verificar saúde da aplicação"
    echo ""
}

# Função para build
build() {
    print_status "Compilando aplicação..."
    go build -o ssw-logs-capture ./cmd/main.go
    print_success "Compilação concluída"
}

# Função para executar localmente
run() {
    print_status "Executando aplicação localmente..."

    # Verificar se existe configuração personalizada
    config_flag=""
    if [ -f "configs/app.yaml" ]; then
        config_flag="-config configs/app.yaml"
        print_status "Usando configuração personalizada"
    fi

    go run ./cmd/main.go $config_flag
}

# Função para testes
test() {
    print_status "Executando testes..."
    go test -v ./...
    print_success "Testes concluídos"
}

# Função para build Docker
docker_build() {
    print_status "Building imagem Docker..."
    docker build -t ssw-logs-capture-go:latest .
    print_success "Imagem Docker criada"
}

# Função para executar com Docker
docker_run() {
    print_status "Iniciando serviços com Docker Compose..."
    docker-compose up --build -d

    print_status "Aguardando serviços iniciarem..."
    sleep 10

    # Verificar se os serviços estão rodando
    if docker-compose ps | grep -q "Up"; then
        print_success "Serviços iniciados com sucesso"
        echo ""
        echo "URLs disponíveis:"
        echo "  - API: http://localhost:8401/health"
        echo "  - Métricas: http://localhost:8001/metrics"
        echo "  - Grafana: http://localhost:3000 (admin/admin)"
        echo "  - Prometheus: http://localhost:9090"
        echo "  - Loki: http://localhost:3100"
    else
        print_error "Falha ao iniciar alguns serviços"
        docker-compose ps
    fi
}

# Função para parar Docker
docker_stop() {
    print_status "Parando containers..."
    docker-compose down
    print_success "Containers parados"
}

# Função para ver logs
docker_logs() {
    service=${2:-log_capturer_go}
    print_status "Mostrando logs do serviço: $service"
    docker-compose logs -f "$service"
}

# Função para limpeza
clean() {
    print_status "Limpando arquivos temporários..."

    # Remover binário
    [ -f "ssw-logs-capture" ] && rm ssw-logs-capture && print_status "Binário removido"

    # Limpar cache do Go
    go clean -cache -modcache -testcache

    # Remover volumes Docker órfãos
    docker system prune -f --volumes 2>/dev/null || true

    print_success "Limpeza concluída"
}

# Função para atualizar dependências
deps() {
    print_status "Atualizando dependências..."
    go mod download
    go mod tidy
    print_success "Dependências atualizadas"
}

# Função para formatar código
fmt() {
    print_status "Formatando código..."
    go fmt ./...
    print_success "Código formatado"
}

# Função para linter
lint() {
    print_status "Executando linter..."

    # Verificar se golangci-lint está instalado
    if ! command -v golangci-lint &> /dev/null; then
        print_warning "golangci-lint não encontrado, instalando..."
        go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    fi

    golangci-lint run ./...
    print_success "Linter concluído"
}

# Função para verificar saúde
health() {
    print_status "Verificando saúde da aplicação..."

    # Verificar API
    if curl -s http://localhost:8401/health > /dev/null; then
        print_success "API está respondendo"
    else
        print_error "API não está respondendo"
    fi

    # Verificar métricas
    if curl -s http://localhost:8001/metrics > /dev/null; then
        print_success "Métricas estão disponíveis"
    else
        print_error "Métricas não estão disponíveis"
    fi

    # Verificar health detalhado
    health_response=$(curl -s http://localhost:8401/health/detailed 2>/dev/null || echo "")
    if echo "$health_response" | grep -q "healthy"; then
        print_success "Health check detalhado: OK"
    else
        print_warning "Health check detalhado indica problemas"
    fi
}

# Processar argumentos
case "${1:-help}" in
    "build")
        build
        ;;
    "run")
        run
        ;;
    "test")
        test
        ;;
    "docker-build")
        docker_build
        ;;
    "docker-run")
        docker_run
        ;;
    "docker-stop")
        docker_stop
        ;;
    "docker-logs")
        docker_logs "$@"
        ;;
    "clean")
        clean
        ;;
    "deps")
        deps
        ;;
    "fmt")
        fmt
        ;;
    "lint")
        lint
        ;;
    "health")
        health
        ;;
    "help"|*)
        show_help
        ;;
esac