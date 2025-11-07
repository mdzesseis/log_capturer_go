# GOROUTINE LEAK - PLANO DE AÇÃO EXECUTÁVEL
**Root Cause**: Reader goroutine deadlock em `stream.Read()` após primeira rotação
**Prioridade**: P0 - CRÍTICO
**Target**: Zero goroutine leak após 30+ minutos de operação

---

## QUICK SUMMARY

**Problema**: `mc.readerWg.Wait()` trava indefinidamente porque reader goroutine bloqueia em `stream.Read()` e não detecta `readerCtx.Done()`.

**Solução**: Implementar timeout wrapper no `stream.Read()` para garantir que reader sempre termina.

**Validação**: Métricas devem mostrar 6+ rotações por container em 30 minutos.

---

## TASK BREAKDOWN

### TASK 1: Implementar Read Timeout Wrapper (P0) ⭐
**Assignee**: golang specialist
**Estimated Time**: 4-6 hours
**Files**: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`

**Mudanças necessárias**:

1. **Substituir lógica de read na função `readContainerLogs()`** (linhas 960-993)

**De (ATUAL - BUGADO)**:
```go
mc.readerWg.Add(1)
go func() {
    defer mc.readerWg.Done()
    defer close(readCh)

    for {
        select {
        case <-readerCtx.Done():
            return
        default:
        }

        localBuf := make([]byte, 8192)
        n, err := stream.Read(localBuf)  // ⚠️ BLOQUEIA AQUI

        // ... rest of code
    }
}()
```

**Para (CORRETO - COM TIMEOUT)**:
```go
mc.readerWg.Add(1)
go func() {
    defer mc.readerWg.Done()
    defer close(readCh)

    readTimeout := 30 * time.Second

    for {
        // Check context before blocking read
        select {
        case <-readerCtx.Done():
            return
        default:
        }

        // Perform read with timeout
        type readOp struct {
            data []byte
            err  error
        }
        readDone := make(chan readOp, 1)

        // Execute read in goroutine to allow timeout
        go func() {
            localBuf := make([]byte, 8192)
            n, err := stream.Read(localBuf)

            var data []byte
            if n > 0 {
                data = make([]byte, n)
                copy(data, localBuf[:n])
            }
            readDone <- readOp{data: data, err: err}
        }()

        // Wait for read OR timeout OR context cancel
        var result readOp
        var ok bool

        select {
        case <-readerCtx.Done():
            // Context cancelled - exit immediately
            return

        case <-time.After(readTimeout):
            // Read timeout - stream may be idle or stuck
            // Return to allow rotation
            cm.logger.WithFields(logrus.Fields{
                "container_id": mc.id,
                "timeout_secs": int(readTimeout.Seconds()),
            }).Debug("Stream read timeout - allowing rotation")
            return

        case result, ok = <-readDone:
            if !ok {
                return
            }
            // Process result below
        }

        // Process read result
        if len(result.data) > 0 {
            select {
            case readCh <- readResult{data: result.data, err: result.err}:
                if result.err != nil {
                    return
                }
            case <-readerCtx.Done():
                return
            }
        } else if result.err != nil {
            select {
            case readCh <- readResult{err: result.err}:
            case <-readerCtx.Done():
            }
            return
        }
    }
}()
```

**Checklist de implementação**:
- [ ] Adicionar constante `readTimeout` (30 segundos)
- [ ] Criar struct `readOp` para resultado do read
- [ ] Envolver `stream.Read()` em goroutine separada
- [ ] Implementar select com 3 casos: context, timeout, read result
- [ ] Adicionar logging para timeouts
- [ ] Garantir que todos paths executam `defer mc.readerWg.Done()`

**Testes unitários necessários**:
```go
// TestReadContainerLogsTimeout - Validar timeout em stream idle
func TestReadContainerLogsTimeout(t *testing.T) {
    // Mock stream que nunca retorna (simula idle)
    // Verificar que reader goroutine termina após 30s
}

// TestReadContainerLogsContextCancel - Validar cancelamento via context
func TestReadContainerLogsContextCancel(t *testing.T) {
    // Mock stream com dados
    // Cancelar context durante read
    // Verificar que reader goroutine termina imediatamente
}

// TestReadContainerLogsNormalRead - Validar leitura normal
func TestReadContainerLogsNormalRead(t *testing.T) {
    // Mock stream com dados
    // Verificar que dados são processados corretamente
}
```

---

### TASK 2: Adicionar Monitoramento de Rotações Paradas (P1)
**Assignee**: observability specialist
**Estimated Time**: 2 hours
**Files**:
- `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`
- `/home/mateus/log_capturer_go/internal/metrics/metrics.go`

**Mudanças necessárias**:

1. **Adicionar métrica de última rotação**:
```go
// Em metrics.go
LastRotationTime = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Name: "log_capturer_container_last_rotation_timestamp",
        Help: "Unix timestamp of last rotation per container",
    },
    []string{"container_id", "container_name"},
)

// Atualizar RecordStreamRotation
func RecordStreamRotation(containerID, containerName string, ageSeconds float64) {
    StreamRotationsTotal.WithLabelValues(containerID, containerName).Inc()
    StreamAgeSeconds.WithLabelValues(containerID).Observe(ageSeconds)
    LastRotationTime.WithLabelValues(containerID, containerName).SetToCurrentTime()  // ⭐ NOVO
}
```

2. **Adicionar verificação de rotação stuck**:
```go
// Em container_monitor.go, na função monitorContainer()
// Adicionar goroutine para monitorar rotações

go func() {
    ticker := time.NewTicker(6 * time.Minute)  // Check 1 min após rotação esperada
    defer ticker.Stop()

    lastRotationCount := 0

    for {
        select {
        case <-containerCtx.Done():
            return
        case <-ticker.C:
            currentRotationCount := mc.rotationCount

            if currentRotationCount == lastRotationCount {
                // Rotação não ocorreu no intervalo esperado
                cm.logger.WithFields(logrus.Fields{
                    "container_id":          mc.id,
                    "container_name":        mc.name,
                    "rotation_count":        currentRotationCount,
                    "expected_interval_min": int(cm.rotationInterval.Minutes()),
                }).Error("Stream rotation stuck - possible deadlock")
                metrics.RecordStreamError("rotation_stuck", mc.id)
            }

            lastRotationCount = currentRotationCount
        }
    }
}()
```

**Checklist**:
- [ ] Adicionar métrica `LastRotationTime`
- [ ] Registrar métrica em `NewMetricsServer()`
- [ ] Atualizar `RecordStreamRotation()` para incluir timestamp
- [ ] Adicionar goroutine de monitoramento em `monitorContainer()`
- [ ] Adicionar alert rule no Grafana

---

### TASK 3: Criar Testes de Validação (P0)
**Assignee**: qa-specialist + continuous-tester
**Estimated Time**: 3 hours
**Files**: `/home/mateus/log_capturer_go/tests/rotation_test.sh` (novo)

**Teste 1: Rotation Continuity Test**
```bash
#!/bin/bash
# tests/rotation_test.sh

set -e

echo "=== ROTATION CONTINUITY TEST ==="

# Configuração
TEST_DURATION_MIN=15
EXPECTED_ROTATIONS=3  # 15min / 5min
CONTAINER_COUNT=10
METRICS_URL="http://localhost:8001/metrics"

echo "Starting $CONTAINER_COUNT test containers..."

# Criar containers de teste
for i in $(seq 1 $CONTAINER_COUNT); do
    docker run -d --name rotation-test-$i \
        alpine sh -c "while true; do echo 'Log message'; sleep 1; done"
done

echo "Waiting $TEST_DURATION_MIN minutes for rotations..."
sleep $((TEST_DURATION_MIN * 60))

echo "Validating rotations..."

failed=0
for i in $(seq 1 $CONTAINER_COUNT); do
    container_id=$(docker inspect -f '{{.Id}}' rotation-test-$i | cut -c1-12)

    rotations=$(curl -s $METRICS_URL | \
                grep "log_capturer_container_stream_rotations_total{container_id=\"$container_id\"" | \
                awk '{print $2}')

    if [ -z "$rotations" ]; then
        echo "FAIL: Container rotation-test-$i has NO rotation metric"
        failed=$((failed + 1))
    elif [ "$rotations" -lt "$EXPECTED_ROTATIONS" ]; then
        echo "FAIL: Container rotation-test-$i has only $rotations rotations (expected $EXPECTED_ROTATIONS)"
        failed=$((failed + 1))
    else
        echo "PASS: Container rotation-test-$i has $rotations rotations"
    fi
done

# Cleanup
echo "Cleaning up test containers..."
for i in $(seq 1 $CONTAINER_COUNT); do
    docker rm -f rotation-test-$i > /dev/null 2>&1 || true
done

if [ $failed -gt 0 ]; then
    echo "FAILED: $failed containers did not rotate correctly"
    exit 1
fi

echo "SUCCESS: All containers rotating correctly"
exit 0
```

**Teste 2: Idle Stream Test**
```bash
#!/bin/bash
# tests/idle_stream_test.sh

set -e

echo "=== IDLE STREAM TEST ==="

METRICS_URL="http://localhost:8001/metrics"

echo "Creating idle container (no logs)..."
docker run -d --name idle-rotation-test alpine sleep 3600

echo "Waiting 11 minutes for 2 rotations..."
sleep 660

container_id=$(docker inspect -f '{{.Id}}' idle-rotation-test | cut -c1-12)

rotations=$(curl -s $METRICS_URL | \
            grep "log_capturer_container_stream_rotations_total{container_id=\"$container_id\"" | \
            awk '{print $2}')

docker rm -f idle-rotation-test > /dev/null 2>&1 || true

if [ -z "$rotations" ]; then
    echo "FAIL: Idle container has NO rotations"
    exit 1
elif [ "$rotations" -lt 2 ]; then
    echo "FAIL: Idle container has only $rotations rotations (expected 2)"
    exit 1
fi

echo "SUCCESS: Idle container rotates correctly ($rotations rotations)"
exit 0
```

**Teste 3: Goroutine Leak Test**
```bash
#!/bin/bash
# tests/goroutine_leak_test.sh

set -e

echo "=== GOROUTINE LEAK TEST ==="

METRICS_URL="http://localhost:8001/metrics"
DURATION_MIN=30
CONTAINER_COUNT=50

# Get baseline
baseline=$(curl -s $METRICS_URL | grep "log_capturer_goroutines " | awk '{print $2}')
echo "Baseline goroutines: $baseline"

# Start load test
echo "Starting $CONTAINER_COUNT containers for $DURATION_MIN minutes..."
./tests/load_test_fase6c.sh

# Get final count
final=$(curl -s $METRICS_URL | grep "log_capturer_goroutines " | awk '{print $2}')
echo "Final goroutines: $final"

# Calculate growth
growth=$((final - baseline))
max_allowed_growth=$((CONTAINER_COUNT * 2))  # 2 per container max

echo "Goroutine growth: $growth (limit: $max_allowed_growth)"

if [ "$growth" -gt "$max_allowed_growth" ]; then
    echo "FAIL: Goroutine growth $growth exceeds limit $max_allowed_growth"
    exit 1
fi

echo "SUCCESS: Goroutine count stable (growth: $growth)"
exit 0
```

**Checklist**:
- [ ] Criar `tests/rotation_test.sh`
- [ ] Criar `tests/idle_stream_test.sh`
- [ ] Criar `tests/goroutine_leak_test.sh`
- [ ] Adicionar testes ao CI/CD pipeline
- [ ] Documentar testes em README

---

### TASK 4: Re-executar FASE 6C com Fix (P0)
**Assignee**: continuous-tester
**Estimated Time**: 1 hour
**Command**: `./load_test_fase6c.sh`

**Critérios de sucesso**:
```
✅ Duração: 30 minutos
✅ Containers: 55
✅ Rotações por container: >= 6 (30min / 5min)
✅ Goroutine growth rate: < 2/min
✅ FD growth rate: < 1/min
✅ Zero "context deadline exceeded" errors
✅ Rotation metric incrementando a cada 5 minutos
```

**Comando de validação**:
```bash
# Durante o teste (T+15min, T+20min, T+25min)
curl -s http://localhost:8001/metrics | grep "log_capturer_container_stream_rotations_total" | tail -5

# Ao final
./validate_fase6c_results.sh
```

**Checklist**:
- [ ] Executar teste FASE 6C
- [ ] Capturar métricas a cada 5 minutos
- [ ] Validar rotações ocorrendo
- [ ] Validar goroutine count estável
- [ ] Validar zero deadlocks
- [ ] Gerar relatório de resultados

---

### TASK 5: Dashboard e Alertas Grafana (P1)
**Assignee**: grafana-specialist
**Estimated Time**: 2 hours
**Files**: `/home/mateus/log_capturer_go/provisioning/dashboards/container_monitor.json`

**Panels necessários**:

1. **Stream Rotations Rate**
```promql
rate(log_capturer_container_stream_rotations_total[5m])
```
Alert: < 0.003 (1 rotation / 5min) por container

2. **Time Since Last Rotation**
```promql
time() - log_capturer_container_last_rotation_timestamp
```
Alert: > 360 (6 minutos sem rotação)

3. **Reader Goroutines per Container**
```promql
log_capturer_goroutines / on() count(log_capturer_container_streams_active)
```
Alert: > 3 goroutines/container (leak)

4. **Rotation Errors**
```promql
rate(log_capturer_container_stream_errors_total{error_type="rotation_stuck"}[5m])
```
Alert: > 0 (qualquer erro de rotação)

**Checklist**:
- [ ] Criar dashboard "Container Stream Health"
- [ ] Adicionar 4 panels acima
- [ ] Configurar alerts no Prometheus
- [ ] Testar alerts com rotação forçada
- [ ] Documentar dashboard

---

## EXECUTION ORDER

### Phase 1: Implementação Crítica (P0) - 1 dia
```
DAY 1:
├─ Morning (4h)
│  ├─ [golang] TASK 1: Implementar Read Timeout Wrapper
│  └─ [qa-specialist] TASK 3: Criar testes de validação
│
└─ Afternoon (4h)
   ├─ [golang] Code review + ajustes
   ├─ [continuous-tester] Executar testes unitários
   └─ [continuous-tester] TASK 4: Re-executar FASE 6C
```

### Phase 2: Monitoramento (P1) - 0.5 dia
```
DAY 2 Morning (4h):
├─ [observability] TASK 2: Monitoramento de rotações paradas
├─ [grafana-specialist] TASK 5: Dashboard e alertas
└─ [continuous-tester] Validação final com todos sistemas
```

---

## SUCCESS CRITERIA

### Testes passando:
- ✅ `rotation_test.sh` - 100% containers com 3+ rotações
- ✅ `idle_stream_test.sh` - Rotação funciona mesmo sem logs
- ✅ `goroutine_leak_test.sh` - Crescimento < 2/min

### Métricas em produção:
- ✅ `log_capturer_container_stream_rotations_total` incrementa a cada 5min
- ✅ `log_capturer_goroutines` estável (growth < 2/min)
- ✅ `file_descriptors` estável (growth < 1/min)
- ✅ Zero alertas de "rotation_stuck"

### Logs:
- ✅ Mensagem "Stream rotated successfully" a cada 5min por container
- ✅ Zero erros "context deadline exceeded"
- ✅ Zero warnings de reader timeout (exceto streams idle)

---

## ROLLBACK PLAN

Se fix causar problemas:

1. **Reverter commit**:
   ```bash
   git revert HEAD
   git push origin main
   ```

2. **Deploy hotfix**:
   ```bash
   docker-compose down
   docker-compose up -d --build
   ```

3. **Monitorar**:
   - Verificar que sistema volta ao estado anterior
   - Analisar logs de erro do fix
   - Criar issue com detalhes

4. **Aplicar Solução 2 (Fallback)**:
   - Timeout no WaitGroup (vaza goroutines mas previne deadlock)
   - Menos ideal mas mantém sistema funcionando

---

## COMUNICAÇÃO

### Stakeholders a notificar:
- [ ] DevOps team - Deploy do fix
- [ ] SRE team - Novos alerts configurados
- [ ] QA team - Novos testes adicionados
- [ ] Product team - Issue resolvida

### Documentação a atualizar:
- [ ] CLAUDE.md - Adicionar lição aprendida sobre stream.Read() blocking
- [ ] TROUBLESHOOTING.md - Adicionar seção sobre goroutine leaks
- [ ] README.md - Atualizar status do projeto

---

## ESTIMATED TIMELINE

- **TASK 1** (P0): 4-6 hours
- **TASK 2** (P1): 2 hours
- **TASK 3** (P0): 3 hours
- **TASK 4** (P0): 1 hour
- **TASK 5** (P1): 2 hours

**Total**: 12-14 hours = 1.5-2 dias úteis

**Target completion**: 2 dias úteis a partir de agora

---

## FINAL NOTES

Este plano resolve o goroutine leak de forma definitiva, atacando a root cause (reader goroutine bloqueada) ao invés dos sintomas.

A solução é conservativa (timeout de 30s) e bem testada, com rollback plan claro.

Após implementação, sistema deve rodar 30+ minutos sem leak, validando que problema foi resolvido.

**Prioridade**: P0 - CRÍTICO - Bloqueia estabilidade em produção.

---

**Preparado por**: workflow-coordinator
**Data**: 2025-11-07
**Status**: READY FOR EXECUTION
