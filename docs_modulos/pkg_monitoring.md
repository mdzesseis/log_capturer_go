# Módulo: pkg/monitoring

## Estrutura

*   `enhanced_metrics.go`: Este arquivo contém o componente `EnhancedMetrics`, que é responsável por coletar e expor um conjunto abrangente de métricas para monitorar a aplicação.

## Como funciona

O módulo `pkg/monitoring` fornece um conjunto de métricas mais avançado e detalhado em comparação com as métricas básicas do módulo `internal/metrics`.

1.  **Inicialização (`NewEnhancedMetrics`):**
    *   Cria uma nova instância de `EnhancedMetrics`.
    *   Inicializa uma ampla gama de métricas do Prometheus, incluindo:
        *   **Métricas da aplicação:** `log_entries_processed_total`, `log_processing_duration_seconds`, `queue_sizes`, `error_rates`, `throughput_gauge`.
        *   **Métricas do sistema:** `goroutine_count`, `file_descriptor_count`, `memory_usage`, `cpu_usage`, `disk_usage`.
        *   **Métricas de desempenho:** `response_time`, `connection_pool_stats`, `compression_ratio`, `batching_stats`.
        *   **Métricas de negócio:** `container_count`, `file_count`, `sink_health`, `leak_detection`.

2.  **Coleta de Métricas do Sistema (`collectSystemMetrics`):**
    *   O método `Start` inicia uma goroutine em segundo plano que chama periodicamente `updateSystemMetrics`.
    *   Esta função coleta métricas de nível de sistema, como uso de memória e contagem de goroutines, e atualiza os medidores (gauges) Prometheus correspondentes.

3.  **Registro de Métricas:**
    *   O módulo fornece um conjunto de métodos para que outros componentes registrem métricas, como:
        *   `RecordLogProcessed`: Registra uma entrada de log processada.
        *   `RecordProcessingDuration`: Registra a duração de uma etapa de processamento de log.
        *   `UpdateQueueSize`: Atualiza o tamanho de uma fila.
        *   `RecordError`: Registra um erro.

4.  **Análise de Desempenho (`PerformanceProfiler`):**
    *   O módulo também inclui um `PerformanceProfiler` que pode ser usado para medir o desempenho de operações específicas.
    *   O método `StartTrace` retorna uma função que, quando chamada, registra a duração da operação rastreada.

## Papel e Importância

O módulo `pkg/monitoring` é essencial para fornecer insights profundos sobre o desempenho, a saúde e o comportamento da aplicação. Seus principais papéis são:

*   **Monitoramento Abrangente:** Fornece um conjunto de métricas muito mais detalhado do que o módulo básico `internal/metrics`, permitindo um monitoramento e análise mais granulares.
*   **Análise de Desempenho:** As métricas de desempenho detalhadas, como tempos de resposta e estatísticas de lote, são inestimáveis para identificar gargalos de desempenho e áreas para otimização.
*   **Depuração:** As métricas detalhadas podem fornecer pistas cruciais para a depuração de problemas complexos.
*   **Insights de Negócio:** As métricas de negócio, como o número de contêineres e arquivos monitorados, podem fornecer insights sobre como a aplicação está sendo usada.

## Configurações

Este módulo não possui nenhuma configuração específica no arquivo `config.yaml`. Ele é habilitado e usado por outros componentes da aplicação.

## Problemas e Melhorias

*   **Redundância:** Como mencionado anteriormente, há alguma sobreposição entre as métricas neste módulo e no módulo `internal/metrics`. Isso poderia ser consolidado.
*   **Estimativa de Uso de CPU:** A função `getFileDescriptorCount` é um placeholder simplificado. Uma implementação mais precisa e específica da plataforma é necessária.
*   **Métricas Personalizadas:** A função `RegisterCustomMetric` é um bom começo, mas poderia ser aprimorada para permitir um registro mais flexível e dinâmico de métricas personalizadas.
*   **Geração de Dashboard:** O módulo poderia ser aprimorado para gerar automaticamente uma configuração de dashboard do Grafana com base nas métricas definidas, o que tornaria muito mais fácil a visualização dos dados.
