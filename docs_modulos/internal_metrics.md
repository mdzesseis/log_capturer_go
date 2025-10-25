# Módulo: internal/metrics

## Estrutura

*   `metrics.go`: Este arquivo define todas as métricas do Prometheus para a aplicação, fornece um `MetricsServer` para expô-las e inclui funções auxiliares para registrar as métricas.

## Como funciona

O módulo `internal/metrics` é responsável por coletar e expor métricas para monitoramento e alertas.

1.  **Definições de Métricas:**
    *   Uma variedade de métricas do Prometheus são definidas como variáveis globais, incluindo:
        *   `LogsProcessedTotal`: Um contador para o número total de logs processados.
        *   `LogsPerSecond`: Um medidor (gauge) para mostrar o throughput atual de processamento de logs.
        *   `DispatcherQueueUtilization`: Um medidor para a utilização da fila do dispatcher.
        *   `ProcessingStepDuration`: Um histograma para medir a duração de cada etapa de processamento.
        *   `LogsSentTotal`: Um contador para logs enviados aos sinks.
        *   `ErrorsTotal`: Um contador para todos os erros, rotulados por componente e tipo de erro.
        *   `FilesMonitored` e `ContainersMonitored`: Medidores para rastrear o número de recursos monitorados.
        *   `ComponentHealth`: Um medidor para indicar o estado de saúde de cada componente.
        *   `MemoryUsage`, `CPUUsage`, `Goroutines`: Medidores para o uso de recursos do sistema.

2.  **Servidor de Métricas (`MetricsServer`):**
    *   A função `NewMetricsServer` cria um novo servidor HTTP dedicado a expor as métricas do Prometheus.
    *   Ele registra todas as métricas definidas com a biblioteca cliente do Prometheus.
    *   O servidor expõe um endpoint `/metrics` que o Prometheus pode raspar (scrape).
    *   Ele também fornece um endpoint `/health` para verificações básicas de saúde.

3.  **Funções Auxiliares:**
    *   O módulo fornece um conjunto de funções auxiliares (ex: `RecordLogProcessed`, `RecordError`) que outros componentes podem usar para registrar métricas facilmente, sem a necessidade de interagir diretamente com a biblioteca cliente do Prometheus.

4.  **Métricas Aprimoradas (`EnhancedMetrics`):**
    *   A struct `EnhancedMetrics` fornece um conjunto mais avançado de métricas, incluindo uso de disco, tempos de resposta, estatísticas do pool de conexões e muito mais.
    *   Ela também inclui um `systemMetricsLoop` que coleta periodicamente métricas de nível de sistema, como contagem de memória e goroutines.

## Papel e Importância

O módulo `internal/metrics` é essencial para a observabilidade da aplicação `log_capturer_go`. Seus papéis principais são:

*   **Monitoramento:** Fornece os dados necessários для monitorar a saúde, o desempenho e o uso de recursos da aplicação em tempo real.
*   **Alertas:** As métricas expostas podem ser usadas pelo Prometheus e pelo Alertmanager para criar alertas para condições críticas (ex: altas taxas de erro, filas cheias, componentes não saudáveis).
*   **Análise de Desempenho:** As métricas detalhadas, especialmente os histogramas, são inestimáveis para identificar gargalos de desempenho e áreas para otimização.
*   **Depuração:** Quando surgem problemas, as métricas podem fornecer pistas cruciais sobre o que está errado.

## Configurações

A seção `metrics` do arquivo `config.yaml` é usada para configurar este módulo. As principais configurações são:

*   `enabled`: Habilita ou desabilita o servidor de métricas.
*   `port`: A porta na qual o servidor de métricas escutará.
*   `path`: O caminho para o endpoint de métricas (geralmente `/metrics`).
*   `namespace`: Um prefixo para todos os nomes de métricas.

## Problemas e Melhorias

*   **Nomenclatura de Métricas:** Os nomes das métricas não são consistentemente nomeados com um namespace. Embora alguns tenham o prefixo `ssw_`, outros não. Uma convenção de nomenclatura consistente deve ser aplicada.
*   **Redundância:** Parece haver alguma sobreposição entre as métricas globais e as métricas na struct `EnhancedMetrics`. Isso poderia ser simplificado.
*   **Documentação:** As strings de ajuda para as métricas são boas, mas poderiam ser ainda mais detalhadas, explicando o que cada rótulo significa e como interpretar a métrica.
*   **Integração com Dashboard:** O módulo poderia ser aprimorado para gerar automaticamente uma configuração básica de dashboard do Grafana com base nas métricas definidas.
