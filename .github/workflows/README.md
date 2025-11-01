# GitHub Actions Workflows

Este diretório contém os workflows de CI/CD para o projeto Log Capturer Go.

## 📋 Workflows Disponíveis

### 1. **CI/CD Pipeline Completa** (`cicd-pipeline.yml`)

Pipeline principal que executa em todos os pushes e PRs para o branch `main`.

#### Jobs:

##### `build-and-test`
Executa testes, validações de qualidade e build da aplicação.

**Steps:**
- ✅ **Race Detector**: Detecta condições de corrida com `go test -race`
- ✅ **Tests com Coverage**: Executa testes com cobertura de código
- ✅ **Coverage Threshold**: Falha se coverage < 70%
- ✅ **Build**: Compila a aplicação
- ✅ **Artifacts**: Faz upload do binário e relatórios

**Validações**:
```bash
# Race conditions
go test -race -short -v ./...

# Coverage mínimo de 70%
COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
if (( $(echo "$COVERAGE < 70" | bc -l) )); then
  exit 1
fi
```

##### `docker-build-and-validate`
Constrói e valida a imagem Docker (apenas no branch main).

**Steps:**
- Build da imagem Docker
- Validação básica da imagem
- Upload de artefatos

##### `deploy`
Simula deploy em ambiente (apenas no branch main).

##### `documentation`
Gera documentação automática da execução da pipeline.

#### Triggers:
- `push` para `main`
- `pull_request` para `main`

---

### 2. **Benchmark Comparison** (`benchmark.yml`)

Workflow para detectar regressões de performance através de benchmarks.

#### Jobs:

##### `benchmark`
Executa benchmarks e compara com o branch main.

**Steps:**
- ✅ Executa benchmarks no branch do PR
- ✅ Executa benchmarks no branch main (para comparação)
- ✅ Compara resultados usando `benchstat`
- ✅ Comenta no PR com análise de performance
- ✅ **FALHA** se detectar regressão > 20%
- ⚠️ **ALERTA** se detectar regressão > 10%

**Exemplo de Output:**
```
📊 Benchmark Comparison

### Comparação com branch main:
name                    old time/op    new time/op    delta
ProcessLogs-8             245µs ± 2%     251µs ± 3%   +2.45%
DispatcherSend-8          123µs ± 1%     118µs ± 2%   -4.07%

### 🔍 Análise de Regressão:
✅ Nenhuma regressão significativa de performance detectada.
```

##### `benchmark-continuous`
Executa benchmarks completos no branch main (baseline).

**Steps:**
- Benchmarks com `-benchtime=10s` para maior precisão
- Salva resultados com timestamp
- Upload de baseline para comparações futuras
- (Opcional) Commit dos resultados no repositório

#### Triggers:
- `pull_request` para `main` - Comparação
- `push` para `main` - Baseline continuous

#### Artifacts:
- Resultados de benchmark (retention: 30 dias para PRs, 90 dias para main)

---

### 3. **Code Review** (`code-review.yml`)

Workflow automatizado de code review (se existente).

---

### 4. **Docker Review** (`docker-review.yml`)

Validação de configurações Docker (se existente).

---

## 🎯 Critérios de Qualidade

### Tests & Coverage
- ✅ **Race Detector**: Obrigatório - deve passar sem warnings
- ✅ **Coverage Threshold**: Mínimo 70% - falha se não atingir
- ✅ **Unit Tests**: Todos os testes devem passar

### Performance
- ✅ **Benchmark Baseline**: Executado em cada merge para main
- ⚠️ **Regressão 10-20%**: Warning no PR (não bloqueia)
- ❌ **Regressão >20%**: FALHA no PR (bloqueia merge)

### Build
- ✅ **Compilation**: Deve compilar sem erros
- ✅ **Docker Build**: Imagem deve ser construída com sucesso

---

## 🚀 Como Usar

### Executar Localmente

#### Race Detector
```bash
go test -race -short -v ./...
```

#### Coverage Check
```bash
go test -v ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

#### Benchmarks
```bash
# Executar benchmarks
go test -bench=. -benchmem -run=^$ ./...

# Comparar com baseline anterior
benchstat old.txt new.txt
```

### Validar Antes de Commit

```bash
# Script de pré-commit sugerido
#!/bin/bash

echo "🔍 Running race detector..."
go test -race -short ./... || exit 1

echo "📊 Checking coverage..."
go test -coverprofile=coverage.out ./...
COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
if (( $(echo "$COVERAGE < 70" | bc -l) )); then
  echo "❌ Coverage ${COVERAGE}% is below 70%"
  exit 1
fi

echo "🏗️ Building..."
go build -o /tmp/test-build ./cmd || exit 1

echo "✅ All checks passed!"
```

---

## 📈 Monitoramento de Performance

### Baseline Storage

Os benchmarks do branch main são salvos como baselines para comparação futura:

```
benchmarks/results/
  ├── bench_20251101_120530.txt
  ├── bench_20251101_150245.txt
  └── bench_20251101_183015.txt
```

### Visualização de Tendências

Para visualizar tendências de performance ao longo do tempo:

```bash
# Comparar múltiplas baselines
benchstat benchmarks/results/bench_*.txt

# Ou usar ferramenta específica
go install golang.org/x/perf/cmd/benchstat@latest
```

---

## 🔧 Configuração

### Secrets Necessários

Para deployment (opcional):
- `DEPLOY_ENV`: Ambiente de deploy

### Permissions

O workflow de benchmark precisa de:
```yaml
permissions:
  contents: read
  pull-requests: write
```

Para comentar resultados nos PRs.

---

## 📝 Melhores Práticas

### 1. **Race Detector em PRs**
Sempre execute o race detector antes de abrir um PR:
```bash
go test -race ./...
```

### 2. **Benchmarks Locais**
Execute benchmarks localmente antes de mudanças significativas:
```bash
# Salvar baseline
go test -bench=. -benchmem ./... > before.txt

# Fazer mudanças...

# Comparar
go test -bench=. -benchmem ./... > after.txt
benchstat before.txt after.txt
```

### 3. **Coverage Incremental**
Ao adicionar código novo, adicione testes correspondentes para manter/melhorar coverage.

### 4. **Review de Benchmark Results**
Sempre revise os resultados de benchmark nos PRs, mesmo se não houver alertas.

---

## 🐛 Troubleshooting

### Falha no Race Detector
```
WARNING: DATA RACE
Read at 0x00c000... by goroutine X:
```

**Solução**: Identificar o acesso concorrente e adicionar proteção (mutex, channels, atomic).

### Coverage Abaixo do Threshold
```
❌ Coverage de 68.5% está abaixo do threshold de 70%
```

**Solução**: Adicionar testes para os pacotes com baixa cobertura:
```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep -v "100.0%"
```

### Benchmark Timeout
```
panic: test timed out after 10m0s
```

**Solução**: Usar `-short` para testes mais rápidos ou ajustar `-benchtime`.

### Regressão de Performance Falso Positivo
Se o benchmark report regressão mas você acredita ser falso positivo:
1. Execute localmente múltiplas vezes
2. Verifique se há variação natural (±5%)
3. Use `-count=10` para maior precisão
4. Considere ajustar o threshold de 20% se necessário

---

## 📊 Métricas de CI/CD

### Objetivos de Performance

| Métrica | Target | Status |
|---------|--------|--------|
| Pipeline Execution Time | < 10 min | ⏱️ |
| Test Success Rate | > 99% | ✅ |
| Coverage Threshold | ≥ 70% | ✅ |
| Race Conditions | 0 | ✅ |
| Performance Regressions | < 1% | 📊 |

---

## 🔄 Changelog

### 2025-11-01 - Fase 12: CI/CD Improvements
- ✅ Adicionado Race Detector ao pipeline principal
- ✅ Adicionado Coverage Threshold (70%)
- ✅ Criado workflow de Benchmark Comparison
- ✅ Configurado benchmark baseline continuous
- ✅ Adicionado comentário automático de benchmarks em PRs

---

## 📚 Referências

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Go Testing Best Practices](https://go.dev/doc/effective_go#testing)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
- [Benchstat Tool](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
- [Go Benchmarking](https://pkg.go.dev/testing#hdr-Benchmarks)

---

**Última Atualização**: 2025-11-01
**Versão**: 1.0
**Mantido por**: DevOps Team
