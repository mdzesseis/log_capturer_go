# Copilot project instructions — log_capturer_go

Go service that captures logs from files and Docker, processes/enriches, and ships to sinks (Loki/local file) with backpressure, DLQ, and strong observability.

## Big picture
- Entry: `cmd/main.go` parses `--config`/`SSW_CONFIG_FILE`, builds `internal/app.App`, then `Run()` blocks on SIGINT/SIGTERM.
- Init order (keep this): Dispatcher → Sinks → Positions → Monitors → Aux (cleanup/leak/disk-buffer/anomaly/enhanced-metrics) → Hot reload/Discovery → HTTP + Metrics.
- API (:8401): `/health`, `/stats`, `/config`, `/config/reload`, `/positions`, `/dlq/stats`, `/dlq/reprocess`, `/metrics` (proxies to metrics server :8001). See `internal/app/handlers.go`.

## Config you’ll touch most
- Main: `configs/config.yaml` (dispatcher, sinks.loki/local_file, file_monitor_service, container_monitor, positions, cleanup, resource_monitoring, disk_buffer, anomaly_detection, hot_reload).
- Pipelines: `processing.pipelines_file` → `configs/pipelines.yaml`; file routing: `file_monitor_service.pipeline_file` → `configs/file_pipeline.yml`.
- Common env overrides: `SSW_CONFIG_FILE`, `SSW_PIPELINES_FILE`, `SSW_FILE_CONFIG`, `LOKI_URL`, `LOG_LEVEL`, `LOG_FORMAT` (defaults applied unless `app.default_configs: false`). See `internal/config/config.go`.
- Hot reload via `hot_reload.*`; POST `/config/reload` applies when enabled.

## Build / run / test
- Local: `go run cmd/main.go --config configs/config.yaml` then GET `http://localhost:8401/health`.
- Docker stack: `docker-compose up -d` (brings app, Loki, Prometheus, Grafana). Ports: 8401/8001/3100/3000/9090.
- Dev helpers: `scripts_go/dev.sh {build|run|test|docker-run|docker-logs|lint|fmt}`.
- Tests: `go test ./...`; load tests in `tests/load` (see README for 10k/25k/50k/100k and sustained). Containerized runner `test_runner` (compose profile `testing`).

## Project patterns (what matters here)
- App wiring (`internal/app`): `New()` loads & validates config, sets logrus, calls `initializeComponents()` in the order above; `Start()/Stop()` implement graceful lifecycle; routes are in `registerHandlers()` with optional security/tracing middleware.
- Dispatcher (`internal/dispatcher/dispatcher.go`): queue + workers, batch send, retries (exp backoff), DLQ (`pkg/dlq`), dedup (`pkg/deduplication`), backpressure (`pkg/backpressure`), degradation, adaptive rate limit; sinks implement `pkg/types.Sink` and are added in `App.initSinks()` + `Dispatcher.AddSink`.
- Monitors: files use `types.FileConfig` from `file_monitor_service`/`files_config` defaults; Docker via `unix:///var/run/docker.sock` with label/name filters, auto-reconnect.
- Ops features behind flags: positions, cleanup, disk buffer (persistent backpressure), resource leak detection, anomaly detection, SLO, tracing, security, goroutine tracking.

## Extension playbook (minimal)
- New endpoint: edit `internal/app/handlers.go`, register in `registerHandlers()`.
- New sink: implement `types.Sink` (Start/Stop/SendBatch) under `internal/sinks`, wire in `App.initSinks()`, call `Dispatcher.AddSink`, add config under `sinks.*` in `configs/config.yaml`.
- Tune delivery: adjust `dispatcher.*` in `configs/config.yaml` and matching fields in `internal/dispatcher/dispatcher.go`.

## Gotchas specific to this repo
- Preserve init order; start sinks before dispatcher; positions before monitors; metrics server can start early.
- Graceful shutdown path: `App.Stop()` cancels, stops HTTP/monitors/aux, drains dispatcher, then stops sinks.
- `/metrics` on API is a proxy to the metrics server (:8001)—don’t break this behavior.
- Defaults vs empty: `files_config.watch_directories` and `include_patterns` apply defaults only when nil; empty slices mean “intentionally empty.”
- Don’t bypass `config.ValidateConfig` (called in `LoadConfig`); many defaults/overrides happen in `internal/config/config.go`.

## Pointers for agents
- Go editing rules: see `.github/instructions/go.instructions.md` and `.github/instructions/gopls.instructions.md` (use gopls workflow for read/edit, then diagnostics/tests).
- Examples live in: dispatcher stats/flow (`internal/dispatcher/*`), HTTP (`internal/app/handlers.go`), config loading/overrides (`internal/config/config.go`).
- Cheat sheet: endpoints + pipelines/labels em `docs/cheatsheets/api-and-pipelines.md`.
