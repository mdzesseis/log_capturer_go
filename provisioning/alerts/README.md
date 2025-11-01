# Prometheus Alert Rules

Este diretório contém as regras de alertas do Prometheus para o Log Capturer Go.

## Arquivos

- **rules.yml**: Definição de todas as regras de alertas
- **alert_config.yml**: Configuração de roteamento e receivers do Alertmanager

## Grupos de Alertas

### 1. log_capturer_critical
Alertas críticos relacionados à saúde do sistema:

#### Goroutines
- **HighGoroutineCount** (critical): > 8000 goroutines por 5 minutos
- **GoroutineCountWarning** (warning): > 5000 goroutines por 5 minutos

#### Memória
- **HighMemoryUsage** (critical): > 80% de uso de memória por 5 minutos
- **MemoryUsageWarning** (warning): > 70% de uso de memória por 5 minutos

#### File Descriptors
- **HighFileDescriptorUsage** (critical): > 80% de FDs em uso por 5 minutos
- **FileDescriptorWarning** (warning): > 70% de FDs em uso por 5 minutos

#### Disco
- **LowDiskSpace** (critical): < 20% de espaço disponível por 5 minutos
- **DiskSpaceWarning** (warning): < 30% de espaço disponível por 5 minutos

#### Circuit Breaker
- **CircuitBreakerOpen** (warning): Circuit breaker aberto por 2 minutos
- **CircuitBreakerStuckOpen** (critical): Circuit breaker aberto por 15+ minutos

#### Taxa de Erros
- **HighErrorRate** (critical): > 1% de taxa de erros por 5 minutos
- **ElevatedErrorRate** (warning): > 0.5% de taxa de erros por 5 minutos

#### Fila do Dispatcher
- **HighQueueUtilization** (critical): > 90% de utilização da fila por 5 minutos
- **QueueUtilizationWarning** (warning): > 70% de utilização da fila por 5 minutos

#### Disponibilidade
- **LogCapturerDown** (critical): Serviço down por 1 minuto
- **NoLogsProcessed** (warning): Nenhum log processado em 10 minutos
- **LowThroughput** (warning): < 10 logs/sec por 10 minutos

#### Dead Letter Queue (DLQ)
- **DLQGrowing** (warning): DLQ cresceu > 100 entradas em 10 minutos
- **DLQCritical** (critical): DLQ contém > 10000 entradas

### 2. log_capturer_performance
Alertas relacionados a performance:

- **HighCPUUsage** (warning): > 80% CPU por 10 minutos
- **HighGCPauseTime** (warning): Alto tempo de pausa do GC
- **SinkLatencyHigh** (warning): P99 latency > 5s para sinks
- **ProcessingLatencyHigh** (warning): P99 latency > 1s para processamento

### 3. log_capturer_resource_leaks
Alertas para detecção de vazamentos de recursos:

- **GoroutineLeakSuspected** (warning): Crescimento > 10 goroutines/min por 30 min
- **MemoryLeakSuspected** (warning): Crescimento > 10MB/min por 30 min
- **FileDescriptorLeakSuspected** (warning): Crescimento > 5 FDs/min por 30 min

## Configuração do Alertmanager

### Receivers

Você pode configurar diferentes receivers no arquivo `alert_config.yml`:

#### Slack
```yaml
receivers:
  - name: 'critical'
    slack_configs:
      - api_url: 'YOUR_SLACK_WEBHOOK_URL'
        channel: '#critical-alerts'
        title: 'Critical Alert: {{ .GroupLabels.alertname }}'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
```

#### Email
```yaml
receivers:
  - name: 'critical'
    email_configs:
      - to: 'oncall@example.com'
        from: 'prometheus@example.com'
        smarthost: 'smtp.example.com:587'
        auth_username: 'prometheus'
        auth_password: 'your_password'
```

#### PagerDuty
```yaml
receivers:
  - name: 'critical'
    pagerduty_configs:
      - service_key: 'YOUR_PAGERDUTY_SERVICE_KEY'
        description: '{{ .GroupLabels.alertname }}'
```

### Rotas

O roteamento de alertas está configurado para:
- **Critical alerts**: Notificação imediata (10s group_wait), repetição a cada 1h
- **Warning alerts**: Notificação normal (30s group_wait), repetição a cada 4h

### Inhibition Rules

Regras de inibição para evitar spam:
1. Alertas warning são inibidos quando o mesmo alerta critical está ativo
2. Todos os alertas são inibidos quando o serviço está down

## Deployment

### Docker Compose

Adicione o Alertmanager ao docker-compose.yml:

```yaml
services:
  alertmanager:
    image: prom/alertmanager:latest
    container_name: alertmanager
    ports:
      - "9093:9093"
    volumes:
      - ./provisioning/alerts/alert_config.yml:/etc/alertmanager/config.yml
      - alertmanager-data:/alertmanager
    command:
      - '--config.file=/etc/alertmanager/config.yml'
      - '--storage.path=/alertmanager'
    networks:
      - monitoring
    restart: unless-stopped

  prometheus:
    # ... configuração existente ...
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - ./provisioning/alerts:/etc/prometheus/alerts
      # ... outros volumes ...
```

### Volumes

Certifique-se de que o Prometheus tenha acesso ao diretório de alertas:
```yaml
volumes:
  alertmanager-data:
```

## Testando Alertas

### 1. Verificar Regras de Alertas

```bash
# Verificar sintaxe das regras
promtool check rules provisioning/alerts/rules.yml

# Verificar configuração do Prometheus
promtool check config prometheus.yml
```

### 2. Simular Alertas

Acesse a interface do Prometheus (http://localhost:9090) e use as queries:

```promql
# Ver alertas ativos
ALERTS

# Ver regras de alertas
count(ALERTS) by (alertname, severity)

# Simular alta contagem de goroutines
go_goroutines{job="log-capturer"} > 8000
```

### 3. Interface do Alertmanager

Acesse http://localhost:9093 para:
- Ver alertas ativos
- Silenciar alertas temporariamente
- Verificar status de notificações

## Manutenção

### Ajustar Thresholds

Edite os thresholds em `rules.yml` conforme necessário:

```yaml
# Exemplo: Ajustar threshold de goroutines
- alert: HighGoroutineCount
  expr: go_goroutines{job="log-capturer"} > 10000  # Mudou de 8000 para 10000
  for: 5m
```

### Adicionar Novos Alertas

1. Edite `rules.yml`
2. Adicione a nova regra ao grupo apropriado
3. Reload do Prometheus: `curl -X POST http://localhost:9090/-/reload`

### Desabilitar Alertas Temporariamente

Use a interface do Alertmanager para silenciar alertas por um período específico.

## Métricas Importantes

Certifique-se de que estas métricas estão sendo expostas:

- `go_goroutines` - Contagem de goroutines
- `process_resident_memory_bytes` - Uso de memória
- `process_open_fds` / `process_max_fds` - File descriptors
- `node_filesystem_avail_bytes` - Espaço em disco
- `circuit_breaker_state` - Estado do circuit breaker
- `logs_errors_total` / `logs_processed_total` - Taxa de erros
- `dispatcher_queue_size` / `dispatcher_queue_capacity` - Utilização da fila
- `dlq_entries_total` - Entradas no DLQ

## Troubleshooting

### Alertas não aparecem

1. Verificar se o Prometheus está carregando as regras:
   ```bash
   curl http://localhost:9090/api/v1/rules
   ```

2. Verificar logs do Prometheus:
   ```bash
   docker logs prometheus
   ```

3. Verificar se as métricas existem:
   ```bash
   curl http://localhost:9090/api/v1/label/__name__/values | grep log_capturer
   ```

### Alertmanager não recebe alertas

1. Verificar conectividade:
   ```bash
   curl http://localhost:9093/-/healthy
   ```

2. Verificar configuração do Alertmanager:
   ```bash
   docker logs alertmanager
   ```

3. Verificar no Prometheus se o Alertmanager está conectado:
   ```bash
   curl http://localhost:9090/api/v1/alertmanagers
   ```

## Referências

- [Prometheus Alerting Rules](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/)
- [Alertmanager Configuration](https://prometheus.io/docs/alerting/latest/configuration/)
- [Alert Notification Template Reference](https://prometheus.io/docs/alerting/latest/notifications/)
