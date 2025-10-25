# GEMINI.md

## Project Overview

`ssw-logs-capture` is a high-performance log aggregation and monitoring system written in Go. It is designed to replace a previous Python-based version, offering significant improvements in performance, memory usage, and stability.

The system monitors logs from Docker containers and local files, processes them through configurable pipelines, and forwards them to various destinations (sinks) such as Loki, Elasticsearch, Splunk, or local files.

**Key Technologies:**

*   Go
*   Docker
*   Prometheus (for metrics)
*   Loki (as a log sink)
*   Elasticsearch (as a log sink)
*   Splunk (as a log sink)

**Architecture:**

The project follows a modular architecture:

*   **`cmd/main.go`**: The main entry point of the application.
*   **`internal/app/app.go`**: Contains the core application logic, including orchestration of components and the HTTP server for APIs.
*   **`internal/config/`**: Handles YAML-based configuration loading and validation.
*   **`internal/dispatcher/`**: Manages log routing, batching, and worker pools for efficient processing.
*   **`internal/monitors/`**: Implements the logic for monitoring file systems and Docker containers for new log entries.
*   **`internal/processing/`**: Defines the log processing pipelines, allowing for transformation and enrichment of logs.
*   **`internal/sinks/`**: Contains implementations for various log output destinations.
*   **`pkg/`**: A collection of utility packages providing functionalities like circuit breakers, compression, file position tracking, and more.

## Building and Running

### Local Development

A helper script is provided for common development tasks:

*   **Build:** `scripts_go/dev.sh build`
*   **Run:** `scripts_go/dev.sh run`
*   **Test:** `scripts_go/dev.sh test`
*   **Format:** `scripts_go/dev.sh fmt`
*   **Lint:** `scripts_go/dev.sh lint`

Alternatively, you can use standard Go commands:

*   **Build:** `go build -o ssw-logs-capture ./cmd/main.go`
*   **Run:** `go run ./cmd/main.go`
*   **Run with custom config:** `go run ./cmd/main.go -config configs/custom.yaml`
*   **Run tests:** `go test ./...`

### Docker Development

The project includes a `docker-compose.yml` file for a Docker-based development environment.

*   **Build and run:** `docker-compose up --build`
*   **View logs:** `docker-compose logs -f log_capturer_go`
*   **Stop:** `docker-compose down`

## Development Conventions

*   **Configuration:** Configuration is managed through YAML files located in the `configs/` directory.
    *   `configs/config.yaml`: Main application configuration.
    *   `configs/pipelines.yaml`: Log processing rules.
    *   `configs/file_pipeline.yml`: File-specific monitoring rules.
*   **Testing:** A comprehensive test runner script is available at `test-scripts/test-runner.sh`.
    *   Run all tests: `./test-scripts/test-runner.sh all`
    *   Run unit tests: `./test-scripts/test-runner.sh unit`
*   **API:** The application exposes a set of RESTful APIs for monitoring and management. Refer to `API.md` for details.
