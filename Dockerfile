# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application (OTIMIZADO - removido -a e -installsuffix)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -trimpath \
    -o ssw-logs-capture \
    ./cmd/main.go

# Final stage
FROM alpine:3.19

# Accept DOCKER_GID as build argument
ARG DOCKER_GID=1001

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata shadow

# Create app user with dynamic docker group
RUN addgroup -g ${DOCKER_GID} docker && \
    adduser -D -u 1000 appuser && \
    adduser appuser docker

# Create directories
RUN mkdir -p /app /logs \
    /app/data/positions \
    /app/data/models \
    /app/data/config_backups \
    /app/configs \
    /app/dlq \
    /app/buffer \
    /app/logs/output \
    /var/log/monitoring_data && \
    chown -R appuser:appuser /app /logs /var/log/monitoring_data && \
    chmod 755 /var/log/monitoring_data

# Copy binary from builder
COPY --from=builder /build/ssw-logs-capture /app/

# Copy configuration files
COPY --chown=appuser:appuser configs/ /app/configs/

# Set working directory
WORKDIR /app

# Switch to app user
USER appuser

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8001/health || exit 1

# Expose ports
EXPOSE 8401 8001

# Default command
CMD ["./ssw-logs-capture", "--config", "/app/configs/config.yaml"]