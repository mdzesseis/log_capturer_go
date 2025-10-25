# Documentação dos Módulos

Esta pasta contém a documentação para os vários módulos da aplicação `log_capturer_go`.

## Módulos Internos

*   [`internal/app`](./internal_app.md): O ponto de entrada principal da aplicação.
*   [`internal/config`](./internal_config.md): Gerencia a configuração da aplicação.
*   [`internal/dispatcher`](./internal_dispatcher.md): Orquestra o fluxo de logs dos monitores para os coletores.
*   [`internal/metrics`](./internal_metrics.md): Coleta e expõe as métricas da aplicação.
*   [`internal/monitors`](./internal_monitors.md): Monitora as fontes de log (arquivos e contêineres).
*   [`internal/processing`](./internal_processing.md): Processa e transforma as entradas de log.
*   [`internal/sinks`](./internal_sinks.md): Envia as entradas de log para os destinos.

## Módulos do Pacote

*   [`pkg/anomaly`](./pkg_anomaly.md): Detecção de anomalias nos dados de log.
*   [`pkg/backpressure`](./pkg_backpressure.md): Gerenciamento de contrapressão para evitar sobrecarga.
*   [`pkg/batching`](./pkg_batching.md): Agrupamento de entradas de log em lotes.
*   [`pkg/buffer`](./pkg_buffer.md): Buffer de disco para persistência de dados.
*   [`pkg/circuit`](./pkg_circuit.md): Implementação de disjuntor para resiliência.
*   [`pkg/cleanup`](./pkg_cleanup.md): Limpeza de arquivos e recursos antigos.
*   [`pkg/compression`](./pkg_compression.md): Compressão de dados de log.
*   [`pkg/deduplication`](./pkg_deduplication.md): Desduplicação de entradas de log.
*   [`pkg/degradation`](./pkg_degradation.md): Degradação graciosa da funcionalidade sob carga.
*   [`pkg/discovery`](./pkg_discovery.md): Descoberta automática de serviços.
*   [`pkg/dlq`](./pkg_dlq.md): Fila de mensagens mortas para entradas com falha.
*   [`pkg/docker`](./pkg_docker.md): Interação com a API do Docker.
*   [`pkg/errors`](./pkg_errors.md): Manipulação padronizada de erros.
*   [`pkg/goroutines`](./pkg_goroutines.md): Detecção de vazamento de goroutine.
*   [`pkg/hotreload`](./pkg_hotreload.md): Recarregamento a quente da configuração.
*   [`pkg/leakdetection`](./pkg_leakdetection.md): Monitoramento de vazamento de recursos.
*   [`pkg/monitoring`](./pkg_monitoring.md): Métricas aprimoradas e monitoramento.
*   [`pkg/persistence`](./pkg_persistence.md): Persistência de lote para recuperação.
*   [`pkg/positions`](./pkg_positions.md): Rastreamento da posição do arquivo de log.
*   [`pkg/ratelimit`](./pkg_ratelimit.md): Limitação de taxa adaptativa.
*   [`pkg/security`](./pkg_security.md): Autenticação, autorização e validação de entrada.
*   [`pkg/selfguard`](./pkg_selfguard.md): Prevenção de loop de feedback.
*   [`pkg/slo`](./pkg_slo.md): Monitoramento de SLO/SLI.
*   [`pkg/task_manager`](./pkg_task_manager.md): Gerenciamento de tarefas em segundo plano.
*   [`pkg/tenant`](./pkg_tenant.md): Gerenciamento de multilocação.
*   [`pkg/throttling`](./pkg_throttling.md): Throttling adaptativo baseado em carga.
*   [`pkg/tracing`](./pkg_tracing.md): Rastreamento distribuído.
*   [`pkg/types`](./pkg_types.md): Definições de tipo de núcleo.
*   [`pkg/validation`](./pkg_validation.md): Validação de dados de entrada.
*   [`pkg/workerpool`](./pkg_workerpool.md): Pool de goroutines de trabalho.
