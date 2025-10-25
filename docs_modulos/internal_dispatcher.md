# Módulo: internal/dispatcher

## Estrutura

*   `dispatcher.go`: Contém a lógica principal para o componente `Dispatcher`, que é responsável por orquestrar o processamento e a entrega de logs.
*   `dispatcher_test.go`: Contém testes unitários e de benchmark para o `Dispatcher`.

## Como funciona

O módulo `internal/dispatcher` é o hub central para as entradas de log na aplicação `log_capturer_go`.

1.  **Inicialização (função `NewDispatcher`):**
    *   Cria uma nova instância do `Dispatcher` com a configuração especificada.
    *   Define valores padrão para o tamanho da fila, número de workers, tamanho do lote e timeouts, se não forem fornecidos.
    *   Inicializa gerenciadores de recursos avançados se estiverem habilitados na configuração:
        *   `DeduplicationManager`: Para evitar o processamento de logs duplicados.
        *   `DeadLetterQueue`: Para armazenar logs que falham no processamento.
        *   `BackpressureManager`: Para lidar com altas cargas e evitar que o sistema seja sobrecarregado.
        *   `DegradationManager`: Para degradar graciosamente o desempenho sob carga pesada.
        *   `AdaptiveRateLimiter`: Para controlar a taxa de requisições de saída para os sinks.

2.  **Manipulação de Entradas de Log (função `Handle`):**
    *   Este é o principal ponto de entrada para novas entradas de log.
    *   Ele realiza uma série de verificações e ações:
        *   **Limitação de Taxa:** Se habilitado, verifica se o log de entrada deve ser limitado.
        *   **Contrapressão (Backpressure):** Se habilitado, verifica a carga do sistema e pode rejeitar ou enfileirar o log com uma prioridade mais baixa.
        *   **Deduplicação:** Se habilitado, verifica se o log é duplicado e o descarta, se for o caso.
        *   **Validação de Timestamp:** Valida o timestamp do log para garantir que ele esteja dentro de um intervalo aceitável.
        *   **Processamento:** Envia a entrada de log para o `LogProcessor` para ser transformada e enriquecida.
        *   **Enfileiramento:** Finalmente, coloca a entrada de log processada em uma fila interna para envio em lote.

3.  **Pool de Workers e Lotes (funções `worker` e `processBatch`):**
    *   O `Dispatcher` usa um pool de goroutines de workers para processar logs em paralelo.
    *   Os workers retiram as entradas de log da fila e as agrupam em lotes.
    *   Os lotes são enviados para todos os sinks configurados (ex: Loki, arquivo local).
    *   Se o envio de um lote falhar, o `Dispatcher` tentará novamente com uma estratégia de backoff exponencial. Se todas as tentativas falharem, o lote é enviado para a Dead Letter Queue (DLQ).

## Papel e Importância

O `Dispatcher` é um componente crítico da aplicação `log_capturer_go`. Suas principais responsabilidades são:

*   **Orquestração:** Atua como o coordenador central, recebendo logs dos monitores e enviando-os para os sinks.
*   **Desempenho:** A arquitetura multi-worker e o mecanismo de lote são fundamentais para o alto throughput da aplicação.
*   **Confiabilidade:** Recursos como novas tentativas, contrapressão e a Dead Letter Queue garantem que os logs não sejam perdidos, mesmo quando os sistemas de destino estão indisponíveis.
*   **Flexibilidade:** Suporta recursos avançados como deduplicação e limitação de taxa, que podem ser habilitados ou desabilitados conforme necessário.

## Configurações

A configuração do `Dispatcher` é feita através da seção `dispatcher` do arquivo `config.yaml`. As configurações principais incluem:

*   `queue_size`: O tamanho da fila de logs interna.
*   `worker_count`: O número de goroutines de workers a serem geradas.
*   `batch_size`: O número máximo de logs a serem incluídos em um único lote.
*   `batch_timeout`: O tempo máximo de espera antes de enviar um lote parcialmente preenchido.
*   `max_retries`: O número de vezes que se deve tentar reenviar um lote com falha.
*   `retry_base_delay`: O atraso inicial entre as novas tentativas.
*   `deduplication_enabled`: Habilita ou desabilita a deduplicação de logs.
*   `dlq_enabled`: Habilita ou desabilita a Dead Letter Queue.

## Problemas e Melhorias

*   **Detecção de Anomalias:** O recurso de detecção de anomalias está temporariamente desabilitado. Ele deve ser reativado e testado.
*   **Contrapressão Específica do Sink:** O mecanismo de contrapressão atual é global. Uma implementação mais avançada poderia aplicar contrapressão por sink.
*   **Filas de Prioridade:** Para casos de uso mais avançados, o `Dispatcher` poderia ser aprimorado para suportar filas de prioridade, permitindo que logs críticos sejam processados mais rapidamente.
*   **Escalonamento Dinâmico de Workers:** O número de workers é atualmente fixo. O `Dispatcher` poderia ser melhorado para escalar dinamicamente o número de workers com base na carga atual.
