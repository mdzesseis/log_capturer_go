# CHECKPOINT - FASE 3: Integration Test

**Data**: 2025-11-06 19:42:00 UTC
**Fase**: 3 de 6
**Status**: BLOQUEADO ⚠️

## Objetivos da Fase
- Re-habilitar Container Monitor
- Monitorar por 10 minutos
- Validar rotação de streams
- Verificar captura de logs

## Resultados do Monitoramento

### Métricas Coletadas
| Tempo | Goroutines | Streams | Rotations | Logs | Health |
|-------|------------|---------|-----------|------|--------|
| 0min  | 1693       | N/A     | N/A       | 2010 | N/A    |
| 5min  | 1875       | N/A     | N/A       | 2068 | N/A    |
| 10min | 2035       | N/A     | N/A       | 2126 | N/A    |

**Note**: Metrics for Active Streams, Total Rotations, and Component Health (container_monitor) were not exposed in Prometheus, indicating a metrics registration issue.

### Análise de Goroutine Growth
- **Inicial**: 1693
- **Final**: 2035
- **Diferença**: 342 goroutines
- **Taxa**: **34.2 goroutines/min**
- **Meta**: < 2/min
- **Status**: FAIL ❌ (exceeds target by 17x)

### Stream Rotation
- **Primeira rotação**: Not observed (metrics not exposed)
- **Intervalo esperado**: 5 minutos (300 seconds)
- **Status**: UNABLE TO VERIFY ⚠️

### Log Capture
- **Logs processados**: 2126 (initial) → 2126 (final)
- **Growth**: 116 logs in 10 minutes
- **Status**: PASS ✅ (logs being captured)

### Component Health
- **container_monitor health metric**: NOT FOUND in Prometheus
- **loki sink health**: 1 (healthy)
- **local_file sink health**: 1 (healthy)
- **Status**: FAIL ❌ (container_monitor health not exposed)

### Erros Encontrados

#### 1. CRITICAL: Goroutine Leak
```
WARNING: Goroutine count increased from 1693 to 2035 in 10 minutes
Growth Rate: 34.2 goroutines/minute (target: < 2/min)
Severity: CRITICAL
```

**Evidence from logs**:
```
{"current_value":1891,"level":"warning","msg":"Goroutine count (1891) exceeded threshold (1000)","severity":"high"}
{"current_value":2035,"level":"warning","msg":"Goroutine count (2035) exceeded threshold (1000)","severity":"critical"}
```

#### 2. CRITICAL: File Descriptor Leak
```
{"current_value":1016,"details":"FD count increased by 1007 from baseline of 9",
 "level":"warning","msg":"Resource leak detected","resource":"File Descriptors",
 "severity":"warning","threshold":1000}
```

#### 3. Missing Metrics
- `log_capturer_active_container_streams` - NOT FOUND
- `log_capturer_stream_rotations_total` - NOT FOUND
- `log_capturer_component_health{component="container_monitor"}` - NOT FOUND

These metrics are not being registered/exposed properly.

## Root Cause Analysis

### Goroutine Leak Source

**Location**: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`
**Function**: `readContainerLogs()` (lines 936-960)

**Issue**: A new goroutine is spawned for EVERY stream read operation:
```go
// Line 936-960
mc.heartbeatWg.Add(1)
go func() {
    defer mc.heartbeatWg.Done()
    for {
        localBuf := make([]byte, 8192)
        n, err := stream.Read(localBuf)
        // ... send to channel
    }
}()
```

**Problem**:
1. Stream rotation occurs every 5 minutes (rotationInterval)
2. Each rotation calls `readContainerLogs()`, spawning a NEW goroutine
3. 8 containers × 2 rotations in 10 min = 16 expected goroutines
4. However, we observed 342 goroutine increase, suggesting:
   - Either rotations are happening MORE frequently than 5 minutes
   - OR the goroutines are NOT being properly cleaned up
   - OR there's another source of goroutine spawning

**Expected Behavior**:
- Each container should have 1 monitor goroutine + 1 read goroutine = 2 per container
- With 8 containers: 16 goroutines total
- After rotation: old goroutines should terminate before new ones start

**Actual Behavior**:
- Goroutines accumulate at ~34/minute
- Suggests goroutines are NOT terminating properly

### File Descriptor Leak Source

**Related to goroutine leak**:
- Each stream.Read() holds a file descriptor to Docker socket
- If goroutines don't terminate, FDs remain open
- 1007 FDs leaked correlates with goroutine leak

### Metrics Not Exposed

**Possible causes**:
1. Metrics registration not called during ContainerMonitor.Start()
2. Metrics registration occurs but labels are incorrect
3. Metrics are being registered but cleared/reset

## Validações
- [x] Sistema iniciou sem erros
- [x] Container Monitor habilitado e rodando
- [ ] Goroutine growth < 2/min - **FAILED** (34.2/min)
- [ ] Rotação ocorreu - **UNABLE TO VERIFY** (metrics missing)
- [x] Logs capturados - **PASSED** (2126 logs processed)
- [ ] Métricas no Prometheus - **PARTIAL** (some metrics missing)
- [ ] Sem status UNHEALTHY - **FAILED** (container_monitor health not exposed)

## Problemas Encontrados

### P1 - CRITICAL: Goroutine Leak (34.2/min)
- **Severity**: CRITICAL
- **Impact**: Memory exhaustion, eventual OOM crash
- **Root Cause**: readContainerLogs spawns goroutine per rotation, not cleaned up
- **Affected Code**: `internal/monitors/container_monitor.go:936-960`

### P2 - CRITICAL: File Descriptor Leak (1007 FDs)
- **Severity**: CRITICAL
- **Impact**: FD exhaustion, "too many open files" error
- **Root Cause**: Related to P1 - goroutines hold FDs open
- **Affected Code**: Same as P1

### P3 - HIGH: Missing Metrics
- **Severity**: HIGH
- **Impact**: Unable to monitor container_monitor health and streams
- **Affected Metrics**:
  - `log_capturer_active_container_streams`
  - `log_capturer_stream_rotations_total`
  - `log_capturer_component_health{component="container_monitor"}`

## Próximos Passos (FASE 4 - BLOQUEADO)

**BLOCKER**: Cannot proceed to Phase 4 (Grafana Dashboard) until goroutine leak is fixed.

**Immediate Actions Required**:
1. **FIX P1**: Refactor `readContainerLogs()` to prevent goroutine accumulation
   - Option A: Reuse single read goroutine per container
   - Option B: Ensure proper cleanup with context cancellation
   - Option C: Use sync read instead of goroutine
2. **FIX P2**: Ensure stream.Close() and FD release
3. **FIX P3**: Register container_monitor metrics properly

**Recommended Approach**:
```go
// Instead of spawning goroutine inside readContainerLogs,
// create ONE reader goroutine per container in monitorContainer()
// and pass data via channel that persists across rotations
```

## Como Retomar

**When resuming**:
1. Assign **go-bugfixer** to fix goroutine leak in `container_monitor.go`
2. Assign **observability** to fix metrics registration
3. After fixes, rebuild and re-run Phase 3 integration test
4. Validate goroutine growth < 2/min
5. Validate metrics are exposed
6. Only then proceed to Phase 4

## Arquivos Modificados
- `configs/config.yaml` - Container Monitor re-enabled (line 99)
- `bin/log_capturer` - Rebuilt with Container Monitor enabled

## Logs Salvos
- `/home/mateus/log_capturer_go/phase3_monitor.log` - 10-minute monitoring data
- Docker logs captured at start of Phase 3

## Test Data

**Monitoring Window**: 10 minutes (600 seconds)
**Check Interval**: 30 seconds
**Total Checks**: 20

**Goroutine Growth Timeline**:
```
Check  1 (  0s): 1693 goroutines
Check  2 ( 30s): 1711 (+18)
Check  3 ( 60s): 1729 (+18)
Check  4 ( 90s): 1747 (+18)
Check  5 (120s): 1766 (+19)
Check  6 (150s): 1783 (+17)
Check  7 (180s): 1802 (+19)
Check  8 (210s): 1819 (+17)
Check  9 (240s): 1837 (+18)
Check 10 (270s): 1855 (+18)
Check 11 (300s): 1875 (+20) <-- Expected rotation
Check 12 (330s): 1891 (+16)
Check 13 (360s): 1911 (+20)
Check 14 (390s): 1927 (+16)
Check 15 (420s): 1945 (+18)
Check 16 (451s): 1965 (+20)
Check 17 (481s): 1981 (+16)
Check 18 (511s): 2001 (+20)
Check 19 (541s): 2019 (+18)
Check 20 (571s): 2035 (+16)

Average growth: ~17 goroutines per 30 seconds = 34/minute
```

**Observations**:
- Growth is STEADY and CONSISTENT (~17-20 every 30s)
- No spike at 300s rotation point
- Suggests continuous spawning, not just at rotations

## Conclusion

Phase 3 **BLOCKED** due to critical goroutine and file descriptor leaks. Container Monitor is functioning (capturing logs), but has severe resource leak issues that will cause production outages.

**Status**: ⚠️ BLOQUEADO - Requires go-bugfixer intervention

**Next Step**: Assign task to **go-bugfixer** to fix goroutine leak before proceeding.
