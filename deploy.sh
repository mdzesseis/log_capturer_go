#!/bin/bash

# SSW Logs Capture Go - Deploy and Management Script
# This script automates user setup, permissions, and Docker Compose management

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
LOG_CAPTURER_USER="log_capturer"
MONITORING_DATA_DIR="/var/log/monitoring_data"
COMPOSE_FILE="docker-compose.yml"

# Function to print colored output
print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_header() {
    echo -e "${BLUE}=====================================
$1
=====================================${NC}"
}

# Function to check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        print_error "Este script deve ser executado como root para configurar usuários e permissões"
        echo "Execute: sudo $0"
        exit 1
    fi
}

# Function to get Docker group GID
get_docker_gid() {
    if getent group docker >/dev/null 2>&1; then
        getent group docker | cut -d: -f3
    else
        print_error "Grupo docker não encontrado. Certifique-se de que o Docker está instalado."
        exit 1
    fi
}

# Function to create log_capturer user
setup_user() {
    print_header "CONFIGURAÇÃO DO USUÁRIO LOG_CAPTURER"

    DOCKER_GID=$(get_docker_gid)
    export DOCKER_GID

    print_status "Docker GID detectado: $DOCKER_GID"

    # Check if user exists
    if id "$LOG_CAPTURER_USER" &>/dev/null; then
        print_status "Usuário $LOG_CAPTURER_USER já existe"

        # Check if user is in required groups
        USER_GROUPS=$(groups "$LOG_CAPTURER_USER" 2>/dev/null | cut -d: -f2)

        print_status "Verificando grupos do usuário..."

        # Add to docker group
        if [[ ! $USER_GROUPS =~ docker ]]; then
            print_status "Adicionando usuário ao grupo docker..."
            usermod -a -G docker "$LOG_CAPTURER_USER"
        else
            print_status "Usuário já está no grupo docker"
        fi

        # Add to syslog group (if exists)
        if getent group syslog >/dev/null 2>&1; then
            if [[ ! $USER_GROUPS =~ syslog ]]; then
                print_status "Adicionando usuário ao grupo syslog..."
                usermod -a -G syslog "$LOG_CAPTURER_USER"
            else
                print_status "Usuário já está no grupo syslog"
            fi
        fi

        # Add to adm group (for log access)
        if getent group adm >/dev/null 2>&1; then
            if [[ ! $USER_GROUPS =~ adm ]]; then
                print_status "Adicionando usuário ao grupo adm..."
                usermod -a -G adm "$LOG_CAPTURER_USER"
            else
                print_status "Usuário já está no grupo adm"
            fi
        fi

    else
        print_status "Criando usuário $LOG_CAPTURER_USER..."

        # Create user with appropriate groups
        GROUPS="docker"

        # Add syslog group if it exists
        if getent group syslog >/dev/null 2>&1; then
            GROUPS="$GROUPS,syslog"
        fi

        # Add adm group if it exists
        if getent group adm >/dev/null 2>&1; then
            GROUPS="$GROUPS,adm"
        fi

        useradd -r -s /bin/false -G "$GROUPS" "$LOG_CAPTURER_USER"
        print_status "Usuário $LOG_CAPTURER_USER criado com grupos: $GROUPS"
    fi

    # Setup directories and permissions
    setup_directories

    print_status "Configuração do usuário concluída!"
}

# Function to setup directories and permissions
setup_directories() {
    print_status "Configurando diretórios e permissões..."

    # Create monitoring data directory
    if [[ ! -d "$MONITORING_DATA_DIR" ]]; then
        print_status "Criando diretório $MONITORING_DATA_DIR..."
        mkdir -p "$MONITORING_DATA_DIR"
    fi

    # Set ownership and permissions for monitoring data directory
    print_status "Configurando permissões para $MONITORING_DATA_DIR..."
    chown -R "$LOG_CAPTURER_USER:$LOG_CAPTURER_USER" "$MONITORING_DATA_DIR"
    chmod 755 "$MONITORING_DATA_DIR"

    # Set permissions for /var/log (read access)
    print_status "Configurando permissões de leitura para /var/log..."

    # Give read access to main log directory
    chmod 755 /var/log

    # Set permissions for common log files and directories
    find /var/log -type d -exec chmod g+rx {} \; 2>/dev/null || true
    find /var/log -type f -exec chmod g+r {} \; 2>/dev/null || true

    # Specific permissions for common log directories
    for LOG_DIR in /var/log/syslog* /var/log/auth* /var/log/kern* /var/log/daemon* /var/log/mail* /var/log/cron*; do
        if [[ -e "$LOG_DIR" ]]; then
            chgrp adm "$LOG_DIR" 2>/dev/null || true
            chmod g+r "$LOG_DIR" 2>/dev/null || true
        fi
    done

    print_status "Diretórios e permissões configurados!"
}

# Function to start Docker Compose
start_compose() {
    print_header "INICIANDO DOCKER COMPOSE"

    if [[ ! -f "$COMPOSE_FILE" ]]; then
        print_error "Arquivo $COMPOSE_FILE não encontrado!"
        exit 1
    fi

    # Export Docker GID for compose
    export DOCKER_GID=$(get_docker_gid)
    print_status "Usando DOCKER_GID=$DOCKER_GID"

    print_status "Iniciando serviços..."
    docker-compose up -d --build

    print_status "Aguardando serviços ficarem prontos..."
    sleep 10

    print_status "Status dos serviços:"
    docker-compose ps

    print_status "Docker Compose iniciado com sucesso!"
    print_warning "API disponível em: http://localhost:8402"
    print_warning "Metrics disponível em: http://localhost:8002"
    print_warning "Grafana disponível em: http://localhost:3000 (admin/admin)"
}

# Function to restart specific service
restart_service() {
    print_header "REINICIAR SERVIÇO"

    echo "Serviços disponíveis:"
    docker-compose ps --services
    echo

    read -p "Digite o nome do serviço para reiniciar: " SERVICE_NAME

    if [[ -z "$SERVICE_NAME" ]]; then
        print_error "Nome do serviço não pode estar vazio!"
        return
    fi

    # Check if service exists
    if ! docker-compose ps --services | grep -q "^${SERVICE_NAME}$"; then
        print_error "Serviço '$SERVICE_NAME' não encontrado!"
        return
    fi

    print_status "Reiniciando serviço '$SERVICE_NAME'..."
    docker-compose restart "$SERVICE_NAME"

    print_status "Serviço '$SERVICE_NAME' reiniciado com sucesso!"
}

# Function to stop Docker Compose
stop_compose() {
    print_header "PARANDO DOCKER COMPOSE"

    print_status "Parando serviços..."
    docker-compose down

    print_status "Docker Compose parado com sucesso!"
}

# Function to clean data
clean_data() {
    print_header "LIMPEZA DE DADOS"

    print_warning "ATENÇÃO: Esta operação irá:"
    print_warning "1. Parar todos os serviços"
    print_warning "2. Remover volumes do Docker"
    print_warning "3. Limpar o diretório $MONITORING_DATA_DIR"
    echo

    read -p "Tem certeza que deseja continuar? (y/N): " -n 1 -r
    echo

    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_status "Operação cancelada."
        return
    fi

    print_status "Parando serviços..."
    docker-compose down -v

    print_status "Limpando diretório de dados..."
    if [[ -d "$MONITORING_DATA_DIR" ]]; then
        rm -rf "${MONITORING_DATA_DIR:?}"/*
        print_status "Diretório $MONITORING_DATA_DIR limpo"
    fi

    # Clean local directories
    for DIR in data dlq batch_persistence debug logs; do
        if [[ -d "$DIR" ]]; then
            print_status "Limpando diretório $DIR..."
            rm -rf "${DIR:?}"/*
        fi
    done

    print_status "Limpeza de dados concluída!"
}

# Function to check service status
check_status() {
    print_header "STATUS DOS SERVIÇOS"

    print_status "Status do Docker Compose:"
    docker-compose ps
    echo

    print_status "Verificando conectividade dos serviços..."

    # Check log_capturer API
    if curl -s -f http://localhost:8402/health >/dev/null 2>&1; then
        print_status "✓ Log Capturer API: Online"
    else
        print_warning "✗ Log Capturer API: Offline"
    fi

    # Check metrics
    if curl -s -f http://localhost:8002/metrics >/dev/null 2>&1; then
        print_status "✓ Metrics: Online"
    else
        print_warning "✗ Metrics: Offline"
    fi

    # Check Grafana
    if curl -s -f http://localhost:3000 >/dev/null 2>&1; then
        print_status "✓ Grafana: Online"
    else
        print_warning "✗ Grafana: Offline"
    fi

    # Check Loki
    if curl -s -f http://localhost:3101/ready >/dev/null 2>&1; then
        print_status "✓ Loki: Online"
    else
        print_warning "✗ Loki: Offline"
    fi
}

# Function to show logs
show_logs() {
    print_header "VISUALIZAR LOGS"

    echo "Serviços disponíveis:"
    docker-compose ps --services
    echo

    read -p "Digite o nome do serviço para visualizar os logs (ou pressione Enter para todos): " SERVICE_NAME

    if [[ -z "$SERVICE_NAME" ]]; then
        print_status "Mostrando logs de todos os serviços..."
        docker-compose logs -f --tail=100
    else
        if ! docker-compose ps --services | grep -q "^${SERVICE_NAME}$"; then
            print_error "Serviço '$SERVICE_NAME' não encontrado!"
            return
        fi

        print_status "Mostrando logs do serviço '$SERVICE_NAME'..."
        docker-compose logs -f --tail=100 "$SERVICE_NAME"
    fi
}

# Main menu
show_menu() {
    clear
    print_header "SSW LOGS CAPTURE GO - GERENCIAMENTO"
    echo "1. Verificar e criar usuário log_capturer"
    echo "2. Iniciar Docker Compose"
    echo "3. Reiniciar serviço específico"
    echo "4. Parar Docker Compose"
    echo "5. Limpar dados (down -v + limpar diretórios)"
    echo "6. Verificar status dos serviços"
    echo "7. Visualizar logs"
    echo "8. Sair"
    echo
}

# Main loop
main() {
    while true; do
        show_menu
        read -p "Escolha uma opção [1-8]: " choice

        case $choice in
            1)
                check_root
                setup_user
                ;;
            2)
                start_compose
                ;;
            3)
                restart_service
                ;;
            4)
                stop_compose
                ;;
            5)
                check_root
                clean_data
                ;;
            6)
                check_status
                ;;
            7)
                show_logs
                ;;
            8)
                print_status "Encerrando..."
                exit 0
                ;;
            *)
                print_error "Opção inválida. Tente novamente."
                ;;
        esac

        echo
        read -p "Pressione Enter para continuar..."
    done
}

# Run main function
main "$@"