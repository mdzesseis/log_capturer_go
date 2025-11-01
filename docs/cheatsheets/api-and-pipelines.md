# Cheat sheet — API endpoints, pipelines e rótulos

Este guia rápido reúne os endpoints expostos pelo serviço, com exemplos mínimos de resposta, e mostra como habilitar pipelines e adicionar rótulos (labels) por configuração.

> Portas padrão: API em :8401, métricas Prometheus em :8001 (o endpoint `/metrics` na API faz proxy para o servidor de métricas).

## Endpoints HTTP (API :8401)

- GET /health
  - Finalidade: status geral da aplicação e de componentes.
  - Exemplo de resposta (campos podem variar por recurso habilitado):
    ```json
    {
      "status": "healthy",
      "timestamp": 1730457600,
      "version": "v0.0.2",
      "services": {
        "dispatcher": {"status": "healthy", "stats": {"...": "..."}},
        "file_monitor": {"status": "healthy", "enabled": true},
        "container_monitor": {"status": "healthy", "enabled": true}
      }
    }
    ```

- GET /stats
  - Finalidade: snapshot detalhado de métricas internas (aplicação, dispatcher, posições, etc.).
  - Exemplo reduzido:
    ```json
    {
      "application": {
        "name": "ssw-logs-capture",
        "version": "v0.0.2",
        "uptime": "1m2s",
        "goroutines": 27,
        "timestamp": 1730457600
      },
      "dispatcher": {"...": "..."},
      "positions": {"...": "..."},
      "resources": {"...": "..."}
    }
    ```

- GET /config
  - Finalidade: configuração sanitizada (sem segredos).
  - Exemplo:
    ```json
    {
      "app": {"name": "ssw-logs-capture", "version": "v0.0.2"},
      "metrics": {"enabled": true, "port": 8001, "path": "/metrics"},
      "processing": {"enabled": true, "pipelines_file": "/app/configs/pipelines.yaml"},
      "dispatcher": {"queue_size": 50000, "worker_count": 6, "batch_size": 500, "batch_timeout": "10s"},
      "sinks": {"loki_enabled": true, "local_file_enabled": true}
    }
    ```

- POST /config/reload
  - Finalidade: acionar hot-reload quando habilitado.
  - Exemplo:
    ```json
    {"status": "success", "message": "Configuration reload triggered successfully."}
    ```

- GET /positions
  - Finalidade: estatísticas do gerenciador de posições (files/containers).
  - Observação: o formato depende da implementação de `PositionBufferManager`; use para diagnóstico.

- GET /dlq/stats
  - Finalidade: estatísticas da Dead Letter Queue quando habilitada.
  - Observação: o formato depende de `dlq.GetStats()`.

- POST /dlq/reprocess
  - Finalidade: mensagem informativa sobre reprocessamento (o loop automático cuida disso quando configurado).
  - Exemplo:
    ```json
    {"status":"info","message":"Manual DLQ reprocessing not implemented. Entries are automatically reprocessed by the background loop.","timestamp":1730457600,"dlq_stats":{}}
    ```

- GET /metrics
  - Finalidade: proxy para as métricas Prometheus do servidor em :8001.
  - Exemplo (trecho em formato de exposição):
    ```
    # HELP process_cpu_seconds_total Total user and system CPU time spent in seconds.
    # TYPE process_cpu_seconds_total counter
    process_cpu_seconds_total 1.23
    ```

### Endpoints de debug
- GET /debug/goroutines
  - Exemplo:
    ```json
    {
      "goroutines": 27,
      "cpus": 8,
      "cgocalls": 0,
      "timestamp": 1730457600,
      "memory": {"alloc": 12345678, "total_alloc": 987654321, "sys": 55555555, "num_gc": 120}
    }
    ```

- GET /debug/memory
  - Exemplo (resumo):
    ```json
    {
      "timestamp": 1730457600,
      "heap": {"alloc": 12345678, "sys": 55555555, "heap_objects": 12345},
      "stack": {"stack_inuse": 524288},
      "gc": {"num_gc": 120, "gc_cpu_fraction": 0.001, "next_gc": 123456789},
      "other": {"mcache_inuse": 0}
    }
    ```

- GET /debug/positions/validate
  - Exemplo:
    ```json
    {"timestamp":1730457600, "status":"healthy", "issues":[], "stats": {"...":"..."}}
    ```

### Endpoints enterprise (quando habilitados)
- GET /slo/status — status de SLOs (formato depende do gerenciador de SLO).
- GET /goroutines/stats — estatísticas detalhadas do tracker de goroutines.
- GET /security/audit — atualmente retorna:
  ```json
  {"message":"Audit log feature not yet implemented","status":"pending"}
  ```

---

## Habilitando pipelines e adicionando rótulos

### 1) Ativar processamento e apontar para o arquivo de pipelines
Em `configs/config.yaml`:
```yaml
processing:
  enabled: true
  pipelines_file: "/app/configs/pipelines.yaml"
```

### 2) Definir pipelines (arquivo `configs/pipelines.yaml`)
Estrutura suportada (descoberta no código):
- Topo: `pipelines:`, `source_mapping:`
- Pipeline: `name`, `description`, `steps` (cada step tem `name`, `type`, `config`, `condition` opcional como regex)
- Tipos de step disponíveis: `regex_extract`, `timestamp_parse`, `json_parse`, `field_add`, `field_remove`, `log_level_extract`

Exemplo mínimo:
```yaml
pipelines:
  - name: default
    description: "Pipeline padrão"
    steps:
      - name: parse_log_level
        type: log_level_extract
        config:
          pattern: "(?i)(debug|info|warn|warning|error|fatal|trace)"
          field: level

      - name: parse_timestamp
        type: timestamp_parse
        config:
          field: message
          auto_detect: true
          use_as_log_timestamp: true

      - name: add_labels
        type: field_add
        config:
          fields:
            environment: production
            tenant: default

  - name: nginx
    description: "Nginx access logs"
    steps:
      - name: extract_fields
        type: regex_extract
        config:
          pattern: "^(\\S+) - - \\[([^]]+)\\] \"(\\S+) (\\S+) (\\S+)\" (\\d{3}) (\\d+)"
          fields: ["client_ip", "ts", "method", "path", "proto", "status", "bytes"]

source_mapping:
  default: ["file", "container"]
  nginx:   ["nginx", "nginx-access", "container_name=nginx"]
```

### 3) Adicionar rótulos (labels)
Há duas formas comuns:
- Via pipeline (`field_add`), como no exemplo acima, que adiciona `environment` e `tenant` à entrada.
- Via configuração do sink Loki (labels padrão no envio):
```yaml
sinks:
  loki:
    enabled: true
    url: "http://loki:3100"
    default_labels:
      service: "ssw-log-capturer"
      component: "dispatcher"
```
> Observação: `default_labels` aparece comentado em `configs/config.yaml`. Descomente/ajuste conforme necessário. Labels adicionados via pipeline vão junto com a entrada processada; labels no sink são anexados no envio ao Loki.

---

## Dicas rápidas
- `/metrics` na API é apenas um proxy — o servidor de métricas real roda em :8001.
- Vários blocos em `/stats` e `/health` aparecem só quando o recurso correspondente está habilitado.
- `source_mapping` direciona quais pipelines aplicar por padrão com base no tipo de origem/padrão (ex.: nome do container).
- Passos com `condition` aplicam regex sobre `message` antes de executar o step.
