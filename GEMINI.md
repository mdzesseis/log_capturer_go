# Gemini Code Assistant Context: `log_capturer_go`

This document provides a comprehensive overview of the `log_capturer_go` project to be used as a contextual reference for the Gemini Code Assistant.

## 1. Project Overview

`log_capturer_go` is a high-performance, enterprise-grade log capturing, processing, and aggregation system written in Go. It is a rewrite of an earlier Python version, designed for improved performance, memory efficiency, and stability in high-throughput environments.

The system monitors log sources, processes the collected logs through a configurable pipeline, and dispatches them to various storage and analysis backends (sinks).

### Core Features:

*   **Multi-Source Monitoring**: Captures logs from both local files and Docker container streams (`stdout`/`stderr`).
*   **Resilient Pipeline**: Features an asynchronous processing queue, batching, automatic retries with exponential backoff, and a Dead Letter Queue (DLQ) for failed logs.
*   **Pluggable Sinks**: Supports sending logs to multiple destinations, including Grafana Loki, Elasticsearch, Splunk, and local files.
*   **High Performance**: Built with Go's native concurrency (goroutines) for efficient parallel processing, capable of handling over 10,000 logs per second.
*   **Observability**: Exposes extensive metrics in Prometheus format and provides detailed health check endpoints.
*   **Advanced Features**: Includes circuit breakers for sink protection, backpressure management, adaptive rate limiting, and experimental features like anomaly detection and multi-tenancy.
*   **Containerized Stack**: The entire application and its dependencies (Loki, Grafana, Prometheus) are managed via Docker Compose for easy deployment and scaling.

### Architecture:

The application follows a modular architecture:

1.  **Monitors**: `file_monitor` and `container_monitor` watch for and collect new log entries.
2.  **Dispatcher**: Receives logs from monitors, places them in an in-memory queue, and manages a pool of workers.
3.  **Processing Pipeline**: Workers take logs from the queue and apply processing steps defined in `pipelines.yaml` (e.g., regex parsing, field manipulation).
4.  **Sinks**: The processed logs are batched and sent to configured sinks (e.g., `LokiSink`, `LocalFileSink`).
5.  **Supporting Systems**: Components like the `PositionManager` track progress to ensure no logs are missed, and the `TaskManager` manages the lifecycle of background tasks.

## 2. Building and Running

The project includes scripts and Docker configurations for a streamlined development and deployment experience.

### Docker (Recommended Method)

The recommended way to run the entire stack is using Docker Compose.

*   **Start all services:**
    ```bash
    docker-compose up --build
    ```
    This command builds the Go application image and starts the `log_capturer_go`, `loki`, `grafana`, and `prometheus` containers.

*   **Stop all services:**
    ```bash
    docker-compose down
    ```

*   **View logs for a specific service:**
    ```bash
    docker-compose logs -f log_capturer_go
    ```

*   **Secure Deployment:** For a more production-like setup, a secure compose file is provided which uses non-root users and binds services to localhost.
    ```bash
    # First, run the setup script (may require sudo)
    ./deploy.sh 1

    # Then, start with the secure configuration
    docker-compose -f docker-compose.secure.yml up -d
    ```

### Local Development

The `scripts_go/dev.sh` script provides helpers for common development tasks.

*   **Build the application:**
    ```bash
    ./scripts_go/dev.sh build
    ```
    This creates a binary named `ssw-logs-capture` in the root directory.

*   **Run the application locally:**
    ```bash
    ./scripts_go/dev.sh run
    ```

*   **Run tests:**
    ```bash
    ./scripts_go/dev.sh test
    ```

*   **Format code:**
    ```bash
    ./scripts_go/dev.sh fmt
    ```

## 3. Development Conventions

*   **Configuration**: The application is configured via a primary YAML file, typically `configs/config.yaml`. The path is specified at runtime with the `--config` flag. The configuration is extensive and is the main way to enable/disable features and tune performance.
*   **Project Structure**: The codebase is organized into standard Go layouts:
    *   `cmd/main.go`: The main application entry point.
    *   `internal/`: Contains all core application logic, neatly separated by domain (e.g., `monitors`, `sinks`, `dispatcher`).
    *   `pkg/`: Contains shared, reusable packages that are not specific to this application's business logic (e.g., `task_manager`, `circuit`, `buffer`).
    *   `configs/`: Holds default and example configuration files.
    *   `provisioning/`: Contains configurations for third-party services like Grafana dashboards and datasources.
    *   `scripts/` & `scripts_go/`: Utility scripts for deployment and development.
*   **Dependency Management**: The project uses Go Modules. Dependencies are defined in `go.mod` and `go.sum`.
*   **Logging**: Structured logging is implemented using `logrus`. The log format (`json` or `text`) and level (`debug`, `info`, `warn`, etc.) are configurable.
*   **Testing**: Unit tests are written alongside the code in `_test.go` files and can be run with the dev script. A `Dockerfile.test` and a `test_runner` service in the compose file suggest a robust testing strategy.
*   **Documentation**: The project maintains high-quality technical documentation in Markdown files. Any new features or significant changes should be documented similarly.
