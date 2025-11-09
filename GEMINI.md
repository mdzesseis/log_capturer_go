# SSW Logs Capture Go

## Project Overview

This project, `ssw-logs-capture`, is a high-performance log aggregation system written in Go. It is designed to capture logs from various sources, including Docker containers and local files, process them through customizable pipelines, and deliver them to multiple destinations (sinks) like Grafana Loki. The system is built with performance, reliability, and observability in mind, featuring adaptive batching, concurrent processing, and a Dead Letter Queue (DLQ) for handling delivery failures.

The project is containerized using Docker and orchestrated with Docker Compose. It includes a comprehensive monitoring setup with Prometheus for metrics and Grafana for visualization.

### Key Technologies

*   **Programming Language:** Go
*   **Containerization:** Docker, Docker Compose
*   **Log Aggregation:** Grafana Loki
*   **Metrics:** Prometheus
*   **Visualization:** Grafana
*   **Messaging:** Kafka (optional, for high-throughput scenarios)

### Architecture

The application follows a modular architecture with the following key components:

*   **Monitors:** Responsible for collecting logs from different sources (e.g., `ContainerMonitor` for Docker, `FileMonitor` for files).
*   **Dispatcher:** A central component that receives logs from monitors, queues them, and dispatches them to the appropriate sinks.
*   **Sinks:** Destinations for the collected logs (e.g., Loki, local files).
*   **Pipelines:** Allow for customizable processing of logs, such as filtering, enrichment, and transformation.
*   **Metrics:** Exposes Prometheus metrics for monitoring the health and performance of the application.
*   **API:** Provides a set of HTTP endpoints for health checks, statistics, and management.

## Building and Running

### Prerequisites

*   Docker
*   Docker Compose

### Running the Application

The recommended way to run the application is by using Docker Compose.

1.  **Clone the repository:**

    ```bash
    git clone <repository-url>
    cd ssw-logs-capture
    ```

2.  **Start the services:**

    ```bash
    docker-compose up -d
    ```

This will start the `log_capturer_go` application along with its dependencies, including Loki, Grafana, and Prometheus.

### Accessing the Services

*   **Application API:** `http://localhost:8401`
*   **Metrics:** `http://localhost:8001/metrics`
*   **Grafana:** `http://localhost:3000` (admin/admin)
*   **Loki:** `http://localhost:3100`

### Building from Source

To build the application from source, you need to have Go installed.

1.  **Build the binary:**

    ```bash
    go build -o ssw-logs-capture ./cmd
    ```

2.  **Run the application:**

    ```bash
    ./ssw-logs-capture --config configs/config.yaml
    ```

## Development Conventions

### Code Style

The project follows standard Go conventions. Use `gofmt` to format your code before committing.

### Testing

The project has a comprehensive test suite. To run the tests, use the following command:

```bash
go test ./...
```

### Linting

The project uses `golangci-lint` for linting. To run the linter, use the following command:

```bash
golangci-lint run
```

### Contributing

Contributions are welcome. Please refer to the `CONTRIBUTING.md` file for more details.
