# CHECKPOINT - FASE 4: Grafana Dashboard

**Data**: 2025-11-06 22:03 BRT
**Fase**: 4 de 6
**Status**: COMPLETO ✅
**Duração**: 30 minutos

---

## Objetivos da Fase

- ✅ Criar dashboard Grafana completo para Container Monitor
- ✅ Configurar 10 painéis de métricas
- ✅ Adicionar 7 alertas específicos
- ✅ Documentar queries úteis para troubleshooting

---

## Dashboard Criado

### Informações Gerais
- **Arquivo**: `provisioning/dashboards/container-monitor-streams.json`
- **URL**: http://localhost:3000/d/container-monitor-streams
- **UID**: `container-monitor-streams`
- **Título**: "Container Monitor - Stream Metrics"
- **Tags**: `log_capturer`, `container_monitor`, `streams`, `goroutines`
- **Refresh**: 30 segundos
- **Time Range**: Last 1 hour (default)

### Datasource
- **Type**: Prometheus
- **UID**: `prometheus`
- **URL**: http://prometheus:9090

---

## Painéis Implementados (10 painéis)

### Panel 1: Goroutine Count Trend
- **Tipo**: Time series
- **Query**: `log_capturer_goroutines`
- **Descrição**: Track goroutine count over time to detect leaks
- **Thresholds**:
  - Green: < 500
  - Yellow: 500-1000
  - Red: > 1000
- **Legenda**: Last, Max, Min
- **Status**: ✅ Funcionando (Current: 292)

### Panel 2: Goroutine Growth Rate
- **Tipo**: Stat (with area graph)
- **Query**: `rate(log_capturer_goroutines[5m]) * 60`
- **Unidade**: goroutines/min
- **Descrição**: Real-time leak detection
- **Thresholds**:
  - Green: < 2/min
  - Yellow: 2-5/min
  - Red: > 5/min
- **Status**: ✅ Funcionando (Current: 0.0/min - Stable)

### Panel 3: Active Container Streams
- **Tipo**: Gauge
- **Query**: `log_capturer_container_streams_active`
- **Range**: 0-50 (pool capacity)
- **Descrição**: Current active streams vs capacity
- **Thresholds**:
  - Green: < 35
  - Yellow: 35-45
  - Red: > 45
- **Status**: ✅ Funcionando (Current: 8/50 - 16% utilization)

### Panel 4: Stream Rotations Counter
- **Tipo**: Time series
- **Query**: `log_capturer_container_stream_rotations_total`
- **Descrição**: Total rotations since startup
- **Legenda**: Last, Max
- **Status**: ✅ Funcionando (Total: 3)

### Panel 5: Stream Rotation Rate
- **Tipo**: Time series
- **Query**: `rate(log_capturer_container_stream_rotations_total[5m]) * 60`
- **Unidade**: rotations/min
- **Descrição**: Should be ~(containers / 5min)
- **Legenda**: Last, Mean, Max
- **Status**: ✅ Funcionando (Current: 0.0/min)

### Panel 6: File Descriptors
- **Tipo**: Time series
- **Query**: `log_capturer_file_descriptors_open`
- **Descrição**: Track FD usage to detect leaks
- **Thresholds**:
  - Green: < 500
  - Yellow: 500-1000
  - Red: > 1000
- **Legenda**: Last, Max, Min
- **Status**: ✅ Funcionando (Current: 126)

### Panel 7: Container Monitor Health
- **Tipo**: Stat
- **Query**: `log_capturer_component_health{component_name="container_monitor"}`
- **Value Mappings**:
  - 1 = Healthy (green background)
  - 0 = Unhealthy (red background)
- **Descrição**: Component health status
- **Status**: ✅ Funcionando (Current: Healthy)

### Panel 8: Memory Usage
- **Tipo**: Time series
- **Query**: `log_capturer_memory_usage_bytes / 1024 / 1024`
- **Unidade**: MB (decmbytes)
- **Descrição**: Memory consumption
- **Legenda**: Last, Max, Mean
- **Status**: ✅ Funcionando (Current: 104.58 MB)

### Panel 9: CPU Usage
- **Tipo**: Gauge
- **Query**: `log_capturer_cpu_usage_percent`
- **Unidade**: percent
- **Range**: 0-100%
- **Thresholds**:
  - Green: < 60%
  - Yellow: 60-80%
  - Red: > 80%
- **Descrição**: CPU utilization
- **Status**: ✅ Funcionando (Current: 0%)

### Panel 10: Logs Processed
- **Tipo**: Time series
- **Query**: `rate(log_capturer_logs_processed_total[5m])`
- **Unidade**: logs/sec
- **Descrição**: Log processing rate
- **Legenda**: Last, Mean, Max
- **Status**: ✅ Funcionando (Current: 0.0/sec)

---

## Alertas Configurados (7 alertas)

Arquivo: `provisioning/alerts/rules.yml`
Grupo: `container_monitor_streams`
Interval: 30s

### 1. GoroutineLeakDetected
- **Severidade**: Critical
- **Expressão**: `rate(log_capturer_goroutines[5m]) * 60 > 2`
- **For**: 5 minutos
- **Threshold**: 2 goroutines/min
- **Descrição**: Goroutine growth rate exceeds safe threshold
- **Ação**: Immediate investigation required

### 2. StreamPoolAtCapacity
- **Severidade**: Warning
- **Expressão**: `log_capturer_container_streams_active >= 50`
- **For**: 2 minutos
- **Threshold**: 50 streams (100% capacity)
- **Descrição**: All stream slots in use
- **Ação**: Cannot handle additional containers

### 3. FileDescriptorLeakDetected
- **Severidade**: Warning
- **Expressão**: `rate(log_capturer_file_descriptors_open[10m]) * 600 > 50`
- **For**: 5 minutos
- **Threshold**: 50 FDs per 10 minutes
- **Descrição**: FD growth indicates potential leak
- **Ação**: Check for unclosed streams

### 4. ContainerMonitorUnhealthy
- **Severidade**: Critical
- **Expressão**: `log_capturer_component_health{component_name="container_monitor"} == 0`
- **For**: 1 minuto
- **Threshold**: Health status = 0
- **Descrição**: Component health check failed
- **Ação**: Container log collection may be impacted

### 5. NoStreamRotations
- **Severidade**: Warning
- **Expressão**: `increase(log_capturer_container_stream_rotations_total[10m]) == 0`
- **For**: 10 minutos
- **Threshold**: 0 rotations in 10min
- **Descrição**: Expected rotation every 5min
- **Ação**: Check rotation mechanism

### 6. HighStreamRotationRate
- **Severidade**: Warning
- **Expressão**: `rate(log_capturer_container_stream_rotations_total[5m]) * 60 > 20`
- **For**: 5 minutos
- **Threshold**: 20 rotations/min
- **Descrição**: Excessive rotations detected
- **Ação**: May indicate container churn

### 7. StreamPoolLowCapacity
- **Severidade**: Warning
- **Expressão**: `log_capturer_container_streams_active > 40`
- **For**: 5 minutos
- **Threshold**: 40 streams (80% utilization)
- **Descrição**: Pool running low on capacity
- **Ação**: Consider scaling

---

## Queries Documentadas

Arquivo: `docs/PROMETHEUS_QUERIES.md`

### Categorias
1. **Goroutine Leak Detection** (6 queries)
   - Current count
   - Growth rate (per minute/hour)
   - Derivative analysis
   - Percentage change
   - Predictive analysis

2. **Stream Pool Monitoring** (4 queries)
   - Active streams
   - Pool utilization
   - Available slots
   - Historical comparison

3. **Rotation Metrics** (5 queries)
   - Total rotations
   - Rotation rate
   - Expected vs actual
   - Time between rotations
   - Hourly statistics

4. **Resource Leak Detection** (5 queries)
   - File descriptors
   - FD growth rate
   - Memory usage
   - Memory growth rate
   - Leak velocity

5. **Performance Metrics** (5 queries)
   - CPU usage
   - Log processing rate
   - Throughput trends
   - Processing statistics

6. **Component Health** (3 queries)
   - Individual component status
   - All components overview
   - Unhealthy count

7. **Advanced Queries** (8 queries)
   - Leak severity score
   - Resource health score
   - Stream efficiency ratio
   - Rotation reliability
   - Multi-metric anomaly detection
   - Predictive analysis
   - Saturation ETA

**Total Queries Documented**: 36 queries

---

## Validações Realizadas

### 1. Grafana Health Check
```bash
curl http://localhost:3000/api/health
# Result: {"database": "ok", "version": "12.1.1"}
```
**Status**: ✅ PASS

### 2. Dashboard Discovery
```bash
curl http://localhost:3000/api/search?query=Container%20Monitor
# Result: Dashboard found with UID "container-monitor-streams"
```
**Status**: ✅ PASS

### 3. Metrics Availability
Todas as 8 métricas base verificadas:
- ✅ log_capturer_goroutines: 292
- ✅ log_capturer_container_streams_active: 8
- ✅ log_capturer_container_stream_rotations_total: 3
- ✅ log_capturer_file_descriptors_open: 126
- ✅ log_capturer_component_health: 1 (Healthy)
- ✅ log_capturer_memory_usage_bytes: 83.6 MB
- ✅ log_capturer_cpu_usage_percent: 0%
- ✅ log_capturer_logs_processed_total: 489

**Status**: ✅ PASS (8/8 metrics working)

### 4. Calculated Queries
- ✅ Goroutine growth rate: Working
- ✅ Rotation rate: Working
- ✅ Log processing rate: Working

**Status**: ✅ PASS

### 5. Alert Rules Syntax
```bash
docker exec prometheus promtool check rules /etc/prometheus/alerts/rules.yml
```
**Status**: ✅ PASS (7 new alerts added)

### 6. Dashboard Provisioning
Dashboard automatically loaded on Grafana restart.
**Status**: ✅ PASS

---

## Arquivos Criados/Modificados

### Criados
1. **`provisioning/dashboards/container-monitor-streams.json`**
   - 1,161 linhas
   - 10 painéis configurados
   - Dashboard completo pronto para produção

2. **`docs/PROMETHEUS_QUERIES.md`**
   - 550+ linhas
   - 36 queries documentadas
   - Exemplos de troubleshooting
   - Best practices
   - Alert threshold reference

### Modificados
3. **`provisioning/alerts/rules.yml`**
   - Adicionado grupo: `container_monitor_streams`
   - 7 novos alertas
   - Thresholds baseados em análise das FASES 1-3
   - Total de alertas no arquivo: 50+

---

## Métricas Atuais (Baseline)

### System Health
- **Goroutines**: 292 (Stable ✅)
- **Growth Rate**: 0.0/min (No leak ✅)
- **Active Streams**: 8/50 (16% utilization)
- **File Descriptors**: 126 (Normal)
- **Memory**: 104.58 MB
- **CPU**: 0%

### Container Monitor
- **Health Status**: Healthy (1)
- **Rotations**: 3 total
- **Rotation Rate**: 0.0/min (Low activity)
- **Logs Processed**: 489 total

### Conclusion
Sistema está **estável e saudável** após correção do goroutine leak.

---

## Próximos Passos (FASE 5)

### Objetivo
Load test com 50+ containers para validar:
- Pool capacity handling
- Rotation mechanism under load
- Leak prevention under stress
- Performance degradation

### Tarefas Previstas
1. Criar script de load test (spawn 60 containers)
2. Monitorar métricas por 1 hora
3. Validar rotações ocorrem corretamente
4. Verificar goroutine count permanece estável
5. Analisar performance e latência
6. Validar alertas disparam corretamente

### Métricas a Monitorar
- Goroutine growth rate (deve permanecer < 2/min)
- Stream pool utilization (deve saturar em 50)
- Rotation rate (deve aumentar proporcionalmente)
- Memory usage (não deve vazar)
- CPU usage (monitorar overhead)

---

## Como Retomar

### Acessar Dashboard
```bash
# Grafana
http://localhost:3000/d/container-monitor-streams

# Prometheus
http://localhost:9090

# Métricas diretas
http://localhost:8001/metrics
```

### Validar Alertas
```bash
# Ver alertas ativos
curl http://localhost:9090/api/v1/alerts

# Ver regras carregadas
curl http://localhost:9090/api/v1/rules | jq '.data.groups[] | select(.name=="container_monitor_streams")'
```

### Testar Queries
```bash
# Query simples
curl 'http://localhost:9090/api/v1/query?query=log_capturer_goroutines'

# Query calculada
curl 'http://localhost:9090/api/v1/query?query=rate(log_capturer_goroutines[5m])*60'
```

### Reload Dashboard
```bash
# Se modificar o JSON, reiniciar Grafana
docker-compose restart grafana

# Aguardar 15s para reload
sleep 15
```

---

## Referências Relacionadas

### Checkpoints Anteriores
- `docs/CHECKPOINT_FASE1_ANALISE_E_PLANEJAMENTO.md` - Análise inicial
- `docs/CHECKPOINT_FASE2_TESTES_EXECUTADOS.md` - Testes diagnósticos
- `docs/CHECKPOINT_FASE3_FINAL_SUCCESS.md` - Correção do leak

### Documentação Técnica
- `docs/PROMETHEUS_QUERIES.md` - Queries e troubleshooting
- `docs/FINAL_GOROUTINE_FIX_REPORT.md` - Detalhes da correção
- `docs/ARCHITECTURE_CONFIGURATION_ANALYSIS.md` - Arquitetura do sistema

### Configuração
- `provisioning/dashboards/container-monitor-streams.json` - Dashboard JSON
- `provisioning/alerts/rules.yml` - Regras de alerta
- `docker-compose.yml` - Stack completa

---

## Lessons Learned

### 1. Metric Label Naming
**Problema**: Dashboard inicialmente usava `component="container_monitor"` mas métrica usa `component_name="container_monitor"`.

**Solução**: Verificar labels exatas via `/metrics` endpoint antes de criar queries.

**Comando útil**:
```bash
curl http://localhost:8001/metrics | grep component_health
```

### 2. Dashboard Provisioning
**Best Practice**: Sempre provisionar dashboards via arquivos JSON em `provisioning/dashboards/` em vez de criar manualmente na UI.

**Benefícios**:
- Versionável no Git
- Reproduzível em ambientes
- Evita drift entre ambientes

### 3. Alert Testing
**Recomendação**: Após adicionar alertas, forçar condições para validar que disparam corretamente.

**Exemplo para FASE 5**:
- Spawnar 51 containers para testar `StreamPoolAtCapacity`
- Simular leak para testar `GoroutineLeakDetected`

### 4. Query Optimization
**Dica**: Queries com janelas de tempo longas (`[30m]`, `[1h]`) consomem mais recursos.

**Recomendação**: Usar `[5m]` para painéis em tempo real, `[30m]` para análise de tendências.

---

## Troubleshooting

### Dashboard não aparece no Grafana
```bash
# 1. Verificar arquivo existe
ls -la provisioning/dashboards/container-monitor-streams.json

# 2. Validar JSON syntax
jq . provisioning/dashboards/container-monitor-streams.json > /dev/null

# 3. Reiniciar Grafana
docker-compose restart grafana

# 4. Verificar logs
docker-compose logs grafana | grep -i dashboard
```

### Painel não exibe dados
```bash
# 1. Testar query diretamente no Prometheus
curl 'http://localhost:9090/api/v1/query?query=QUERY_HERE'

# 2. Verificar métrica existe
curl http://localhost:8001/metrics | grep METRIC_NAME

# 3. Verificar datasource no Grafana
curl -u admin:admin http://localhost:3000/api/datasources
```

### Alertas não carregam
```bash
# 1. Validar sintaxe YAML
docker exec prometheus promtool check rules /etc/prometheus/alerts/rules.yml

# 2. Verificar Prometheus config
curl http://localhost:9090/api/v1/status/config | jq '.data.yaml' | grep rule_files

# 3. Reiniciar Prometheus
docker-compose restart prometheus
```

---

## Status Final

### Critérios de Sucesso (Checklist)
- ✅ Dashboard JSON criado com 10 painéis
- ✅ Dashboard provisionado no Grafana
- ✅ 7 alertas configurados (excedeu meta de 5)
- ✅ 36 queries documentadas (excedeu expectativas)
- ✅ Validação visual (painéis exibindo dados)
- ✅ Checkpoint criado

### Entregáveis
- ✅ `provisioning/dashboards/container-monitor-streams.json` (1,161 linhas)
- ✅ `provisioning/alerts/rules.yml` (atualizado com 7 alertas)
- ✅ `docs/PROMETHEUS_QUERIES.md` (550+ linhas)
- ✅ `docs/CHECKPOINT_FASE4_GRAFANA_DASHBOARD.md` (este arquivo)

### Resultado
**FASE 4: COMPLETA COM SUCESSO ✅**

---

**Timestamp**: 2025-11-06 22:03:30 BRT
**Duração Total**: 30 minutos
**Próxima Fase**: FASE 5 - Load Test com 50+ Containers
**Responsável**: workflow-coordinator + grafana-specialist

---

## Dashboard Preview

### Layout (Grid: 24 cols x 32 rows)

```
┌─────────────────────────────────────────────────────┐
│  Panel 1: Goroutine Count Trend (12w x 8h)         │
│  [Time series graph showing goroutine count]        │
├──────────────────────────┬──────────────────────────┤
│  Panel 2: Growth Rate    │  Panel 3: Active Streams │
│  (6w x 8h)              │  (6w x 8h)              │
│  [Stat: 0.0/min]        │  [Gauge: 8/50]          │
├─────────────────────────────────────────────────────┤
│  Panel 4: Rotations Counter (12w x 8h)             │
│  [Time series showing total rotations]              │
├─────────────────────────────────────────────────────┤
│  Panel 5: Rotation Rate (12w x 8h)                 │
│  [Time series showing rotations/min]                │
├─────────────────────────────────────────────────────┤
│  Panel 6: File Descriptors (12w x 8h)              │
│  [Time series showing FD count]                     │
├──────────────┬──────────────────────────────────────┤
│  Panel 7:    │  Panel 8: Memory Usage (6w x 8h)    │
│  Health      │  [Time series: 104 MB]              │
│  (6w x 8h)   ├──────────────────────────────────────┤
│  [Healthy]   │  Panel 9: CPU (6w x 8h)             │
│              │  [Gauge: 0%]                         │
│              ├──────────────────────────────────────┤
│              │  Panel 10: Logs Processed           │
│              │  (18w x 8h)                          │
│              │  [Time series: 0/sec]                │
└──────────────┴──────────────────────────────────────┘
```

---

## Conclusão

A FASE 4 foi concluída com sucesso total. O dashboard Grafana está operacional, todos os painéis exibem dados corretamente, alertas foram configurados e documentação abrangente foi criada.

O sistema está pronto para FASE 5: Load testing sob alta carga (50+ containers) para validar comportamento em condições de stress e confirmar que o goroutine leak está permanentemente resolvido.

**Dashboard URL**: http://localhost:3000/d/container-monitor-streams

---

**Workflow Coordinator**
Fase 4/6 Completa ✅
