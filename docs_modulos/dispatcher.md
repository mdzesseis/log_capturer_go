# Módulo Dispatcher

## Estrutura e Operação

O módulo `dispatcher` é o coração do sistema de processamento de logs do `log_capturer_go`. Ele atua como um orquestrador central que recebe logs dos monitores, os enfileira e os distribui para os `sinks` (destinos) de forma eficiente e confiável.

### Principais Componentes da Estrutura:

- **`Dispatcher` Struct**: A estrutura principal que gerencia uma fila de logs, um pool de workers e a lista de sinks. Ela também integra funcionalidades avançadas como backpressure, rate limiting e deduplicação.
- **`DispatcherConfig` Struct**: Contém todas as configurações do dispatcher, como o tamanho da fila, número de workers, tamanho dos lotes (batches) e políticas de retry.
- **`dispatchItem` Struct**: Representa um item na fila de processamento, contendo o log e informações de controle, como o número de tentativas de envio.
- **`worker` Goroutines**: São os trabalhadores que consomem os logs da fila, os agrupam em lotes e os enviam para os sinks.

### Fluxo de Operação:

1.  **Recebimento**: O `dispatcher` recebe os logs dos monitores (de arquivos ou containers) através do método `Handle`.
2.  **Enfileiramento**: Os logs são colocados em uma fila interna (`chan dispatchItem`) para serem processados de forma assíncrona.
3.  **Processamento em Lote**: Os `workers` retiram os logs da fila e os agrupam em lotes (`batches`) para otimizar o envio.
4.  **Envio**: Cada lote de logs é enviado para todos os `sinks` configurados (Loki, arquivo local, etc.).
5.  **Tratamento de Falhas**: Se o envio para um sink falhar, o `dispatcher` implementa uma política de `retry` com `exponential backoff`. Se todas as tentativas falharem, o log pode ser enviado para uma Dead Letter Queue (DLQ).

## Papel e Importância

O `dispatcher` é **crucial para a performance e a confiabilidade** do `log_capturer_go`. Ele desacopla a coleta de logs do envio, permitindo que o sistema absorva picos de logs sem sobrecarregar os destinos.

Suas principais responsabilidades são:

- **Alta Performance**: Através do processamento paralelo com múltiplos workers e do envio em lote, o dispatcher garante uma alta taxa de transferência de logs.
- **Confiabilidade**: A implementação de retries e a Dead Letter Queue (DLQ) garantem que os logs não sejam perdidos em caso de falhas temporárias nos sinks.
- **Gerenciamento de Carga**: Com funcionalidades como backpressure e rate limiting, o dispatcher protege os sistemas de destino contra sobrecarga.

## Configurações Aplicáveis

As configurações do `dispatcher` são definidas na seção `dispatcher` do arquivo `config.yaml`:

- **`queue_size`**: O tamanho da fila interna para armazenar logs antes do processamento.
- **`worker_count`**: O número de goroutines que processarão os logs em paralelo.
- **`batch_size`**: O número máximo de logs a serem agrupados em um único lote.
- **`batch_timeout`**: O tempo máximo que um lote pode esperar antes de ser enviado, mesmo que não esteja cheio.
- **`max_retries`**: O número máximo de tentativas de envio para um lote que falhou.
- **`retry_base_delay`**: O tempo de espera inicial antes da primeira tentativa de reenvio.
- **`dlq_enabled`**: Habilita ou desabilita a Dead Letter Queue.

## Problemas e Melhorias

### Problemas Potenciais:

- **Consumo de Memória**: Uma `queue_size` muito grande pode consumir uma quantidade significativa de memória, especialmente se os sinks estiverem lentos.
- **Sincronização de Workers**: A coordenação entre múltiplos workers para o envio de lotes pode introduzir complexidade e potenciais condições de corrida se não for bem gerenciada.
- **Perda de Dados**: Se a aplicação for encerrada abruptamente, os logs que estão na fila em memória podem ser perdidos se não houver um mecanismo de persistência.

### Sugestões de Melhorias:

- **Fila Persistente**: Implementar uma fila persistente em disco (como o `pkg/buffer`) para garantir que os logs não sejam perdidos em caso de reinicialização da aplicação.
- **Batching Adaptativo**: Tornar o tamanho do lote e o timeout adaptáveis com base na latência e na taxa de transferência dos sinks, otimizando a performance em tempo real.
- **Priorização de Logs**: Implementar um sistema de prioridades na fila para que logs mais críticos (como erros) sejam processados antes de logs de menor prioridade (como debug).
- **Métricas Detalhadas por Worker**: Adicionar métricas para cada worker individualmente para facilitar a identificação de gargalos ou workers lentos.
