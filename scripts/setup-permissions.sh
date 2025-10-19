#!/bin/bash

# =============================================================================
# Setup Permissions Script for SSW Logs Capture Go
# =============================================================================
# This script sets up proper permissions for running log_capturer_go securely
# without requiring root privileges or privileged mode.
# =============================================================================

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
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

# Check if running as root
check_root() {
    if [[ $EUID -eq 0 ]]; then
        log_error "This script should NOT be run as root for security reasons."
        log_info "Please run as a regular user with sudo privileges."
        exit 1
    fi
}

# Get Docker group ID
get_docker_gid() {
    local docker_gid
    if getent group docker > /dev/null 2>&1; then
        docker_gid=$(getent group docker | cut -d: -f3)
        echo "$docker_gid"
    else
        log_error "Docker group not found. Please ensure Docker is installed."
        exit 1
    fi
}

# Create necessary directories
create_directories() {
    log_info "Creating necessary directories..."

    local dirs=(
        "/var/log/monitoring_data"
        "/var/log/monitoring_data/logs"
        "/var/log/monitoring_data/data"
        "/var/log/monitoring_data/dlq"
        "/var/log/monitoring_data/batch_persistence"
        "/var/log/monitoring_data/debug"
    )

    for dir in "${dirs[@]}"; do
        if [[ ! -d "$dir" ]]; then
            sudo mkdir -p "$dir"
            log_success "Created directory: $dir"
        else
            log_info "Directory already exists: $dir"
        fi
    done
}

# Set up permissions
setup_permissions() {
    log_info "Setting up permissions..."

    local docker_gid
    docker_gid=$(get_docker_gid)

    # Set ownership of monitoring directories
    sudo chown -R "$USER:$docker_gid" /var/log/monitoring_data
    sudo chmod -R 755 /var/log/monitoring_data

    # Ensure Docker socket is accessible
    if [[ -S /var/run/docker.sock ]]; then
        sudo chown root:"$docker_gid" /var/run/docker.sock
        sudo chmod 660 /var/run/docker.sock
        log_success "Docker socket permissions configured"
    else
        log_warning "Docker socket not found at /var/run/docker.sock"
    fi

    # Add current user to docker group if not already
    if ! groups "$USER" | grep -q docker; then
        sudo usermod -aG docker "$USER"
        log_success "Added $USER to docker group"
        log_warning "You need to log out and log back in for group changes to take effect"
    else
        log_info "User $USER is already in docker group"
    fi
}

# Create .env file with proper settings
create_env_file() {
    log_info "Creating .env file..."

    local docker_gid
    docker_gid=$(get_docker_gid)

    cat > .env << EOF
# =============================================================================
# Environment Configuration for SSW Logs Capture Go
# =============================================================================

# Docker Configuration
DOCKER_GID=${docker_gid}

# Security Configuration
GRAFANA_PASSWORD=admin123

# Logging Configuration
LOG_LEVEL=info
LOG_FORMAT=json
DEBUG_MODE=false

# Network Configuration (localhost only for security)
SERVER_HOST=127.0.0.1
METRICS_HOST=127.0.0.1

# Application Configuration
SSW_CONFIG_FILE=/app/configs/config.yaml
SSW_PIPELINES_FILE=/app/configs/pipelines.yaml
SSW_FILE_CONFIG=/app/configs/file_pipeline.yml

# Loki Configuration
LOKI_URL=http://loki:3100
EOF

    log_success "Created .env file with Docker GID: $docker_gid"
}

# Security recommendations
show_security_recommendations() {
    log_info "Security Recommendations:"
    echo
    echo "1. 🔒 Use the secure docker-compose configuration:"
    echo "   docker-compose -f docker-compose.secure.yml up -d"
    echo
    echo "2. 🌐 Services are bound to localhost only (127.0.0.1)"
    echo "   - Use SSH tunneling for remote access"
    echo "   - Configure reverse proxy with TLS for production"
    echo
    echo "3. 🛡️ Container Security Features Enabled:"
    echo "   - no-new-privileges"
    echo "   - capability dropping"
    echo "   - read-only filesystems where possible"
    echo "   - tmpfs for temporary data"
    echo
    echo "4. 👤 Non-root execution:"
    echo "   - Application runs as user 1000 (appuser)"
    echo "   - Docker access through group membership only"
    echo
    echo "5. 📁 Named volumes for data persistence"
    echo "   - Better security than bind mounts"
    echo "   - Easier backup and migration"
    echo
}

# Test Docker access
test_docker_access() {
    log_info "Testing Docker access..."

    if docker ps > /dev/null 2>&1; then
        log_success "Docker access confirmed"
    else
        log_error "Cannot access Docker. Please check:"
        echo "  1. Docker is running"
        echo "  2. User is in docker group"
        echo "  3. You have logged out/in after group changes"
        return 1
    fi
}

# Main execution
main() {
    echo "==============================================="
    echo "SSW Logs Capture Go - Permission Setup"
    echo "==============================================="
    echo

    check_root

    log_info "Starting permission setup..."

    create_directories
    setup_permissions
    create_env_file
    test_docker_access

    echo
    log_success "Permission setup completed successfully!"
    echo

    show_security_recommendations

    echo
    log_info "Next steps:"
    echo "1. Review the generated .env file"
    echo "2. Start services with: docker-compose -f docker-compose.secure.yml up -d"
    echo "3. Check logs with: docker-compose -f docker-compose.secure.yml logs -f"
    echo
}

# Run main function
main "$@"