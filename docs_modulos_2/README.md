# Documentação de Módulos - Log Capturer Go

Este diretório contém documentação detalhada para cada módulo do sistema log_capturer_go, organizada por categoria.

## Módulos Documentados

### 📁 Internal Modules (Módulos Principais)

1. **[internal_app.md](internal_app.md)** - Módulo App (internal/app)
   - Orquestrador central da aplicação
   - Gerenciamento de ciclo de vida
   - Coordenação de todos os componentes

2. **[internal_config.md](internal_config.md)** - Módulo Config (internal/config)
   - Carregamento e validação de configuração
   - Override por variáveis de ambiente
   - Hot reload de configuração

3. **[internal_dispatcher.md](internal_dispatcher.md)** - Módulo Dispatcher (internal/dispatcher)
   - Núcleo de orquestração do pipeline
   - Worker pools e processamento paralelo
   - Distribuição para sinks

4. **[internal_metrics.md](internal_metrics.md)** - Módulo Metrics (internal/metrics)
   - Exposição de métricas Prometheus
   - Observabilidade completa do sistema
   - Monitoramento de performance

5. **[internal_monitors.md](internal_monitors.md)** - Módulo Monitors (internal/monitors)
   - Captura de logs de múltiplas fontes
   - FileMonitor e ContainerMonitor
   - Auto-descoberta e position tracking

6. **[internal_processing.md](internal_processing.md)** - Módulo Processing (internal/processing)
   - Transformação e enriquecimento de logs
   - Pipelines configuráveis
   - Steps de processamento modulares

7. **[internal_sinks.md](internal_sinks.md)** - Módulo Sinks (internal/sinks)
   - Entrega final para destinos
   - Múltiplos sinks (Loki, LocalFile, ES, Splunk)
   - Batching e compressão

### 📦 Package Modules (Módulos Secundários)

8. **[pkg_types.md](pkg_types.md)** - Módulo Types (pkg/types)
   - Definições de estruturas de dados centrais
   - Interfaces e configurações
   - LogEntry e tipos de configuração

9. **[pkg_positions.md](pkg_positions.md)** - Módulo Positions (pkg/positions)
   - Rastreamento persistente de posições
   - Recuperação sem perda de dados
   - Position buffer management

10. **[pkg_compression.md](pkg_compression.md)** - Módulo Compression (pkg/compression)
    - Compressão adaptativa HTTP
    - Múltiplos algoritmos (gzip, zstd, lz4)
    - Seleção automática de algoritmo

11. **[pkg_docker.md](pkg_docker.md)** - Módulo Docker (pkg/docker)
    - Connection pooling para Docker API
    - Health monitoring de conexões
    - Retry logic e circuit breaker

## Módulos Pendentes de Documentação

### Pkg Modules Restantes (20+ módulos)

- **pkg/dlq** - Dead Letter Queue para logs com falha
- **pkg/validation** - Validação de timestamps e dados
- **pkg/security** - Autenticação, autorização e auditoria
- **pkg/tracing** - Distributed tracing OpenTelemetry
- **pkg/slo** - Service Level Objectives monitoring
- **pkg/goroutines** - Rastreamento de goroutines e leak detection
- **pkg/leakdetection** - Detecção de vazamentos de recursos
- **pkg/monitoring** - Monitoramento avançado e alertas
- **pkg/cleanup** - Limpeza automática de disco e recursos
- **pkg/buffer** - Buffer persistente em disco
- **pkg/batching** - Batching adaptativo de logs
- **pkg/circuit** - Circuit breaker pattern
- **pkg/backpressure** - Controle de fluxo e backpressure
- **pkg/ratelimit** - Rate limiting avançado
- **pkg/throttling** - Throttling de recursos
- **pkg/deduplication** - Deduplicação de logs
- **pkg/degradation** - Degradação graceful de serviços
- **pkg/discovery** - Service discovery automático
- **pkg/selfguard** - Proteção contra feedback loops
- **pkg/hotreload** - Hot reload de configuração
- **pkg/task_manager** - Gerenciamento de tarefas background
- **pkg/tenant** - Multi-tenancy e isolamento
- **pkg/workerpool** - Worker pools avançados
- **pkg/persistence** - Persistência de estado
- **pkg/errors** - Sistema de erros estruturados
- **pkg/anomaly.disabled** - Detecção de anomalias (desabilitado)

## Estrutura de Cada Documento

Cada documento de módulo segue uma estrutura padronizada:

1. **Estrutura e Responsabilidades** - Visão geral e arquivos principais
2. **Funcionamento** - Como o módulo funciona internamente
3. **Papel e Importância** - Qual a importância no sistema
4. **Configurações Aplicáveis** - Exemplos de configuração YAML
5. **Problemas Conhecidos** - Issues e limitações conhecidas
6. **Melhorias Propostas** - Implementações futuras e otimizações
7. **Métricas Expostas** - Métricas Prometheus disponíveis
8. **Exemplo de Uso** - Código de exemplo prático
9. **Dependências** - Bibliotecas e módulos dependentes

## Como Usar Esta Documentação

1. **Para Desenvolvedores**: Use para entender a arquitetura e implementar features
2. **Para Operadores**: Foque nas seções de configuração e métricas
3. **Para Troubleshooting**: Consulte "Problemas Conhecidos" e métricas
4. **Para Planejamento**: Veja "Melhorias Propostas" para roadmap futuro

## Convenções

- **✅ Implementado**: Funcionalidade totalmente implementada
- **🚧 Em Desenvolvimento**: Funcionalidade em implementação
- **📝 Planejado**: Funcionalidade planejada para implementação futura
- **⚠️ Limitações**: Limitações conhecidas do módulo

## Contribuindo

Para contribuir com a documentação:

1. Mantenha a estrutura padronizada
2. Inclua exemplos práticos de código
3. Documente configurações YAML completas
4. Atualize métricas quando adicionar novas
5. Mantenha seção de "Problemas Conhecidos" atualizada

## Próximos Passos

1. **Fase 1**: Completar documentação dos módulos pkg restantes
2. **Fase 2**: Adicionar diagramas arquiteturais detalhados
3. **Fase 3**: Criar guias de troubleshooting específicos
4. **Fase 4**: Desenvolver tutoriais de configuração por cenário

---

*Documentação gerada para log_capturer_go - Sistema de captura e processamento de logs de alta performance*