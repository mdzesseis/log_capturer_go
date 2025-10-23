# Módulo Batching

## Estrutura e Operação

O módulo `batching` implementa uma estratégia de agrupamento de logs em lotes (batches) de forma adaptativa. O objetivo é otimizar a performance de envio, ajustando dinamicamente o tamanho dos lotes e o tempo de espera (flush delay) com base na latência e na taxa de transferência observadas.

### Principais Componentes da Estrutura:

- **`AdaptiveBatcher`**: A estrutura principal que gerencia a lógica de batching adaptativo. Ela mantém um buffer de logs e decide quando "fechar" e enviar um lote.
- **`AdaptiveBatchConfig`**: Contém as configurações para o batcher, como os tamanhos mínimo e máximo dos lotes, os delays de flush e os alvos de performance (latência e throughput).
- **`adaptationLoop`**: Uma goroutine que é executada periodicamente para analisar as métricas de performance e ajustar os parâmetros de batching (tamanho e delay).

### Fluxo de Operação:

1.  **Adição de Logs**: Os logs são adicionados um a um ao `AdaptiveBatcher` através do método `Add()`.
2.  **Formação de Lote**: Os logs são acumulados em um buffer interno.
3.  **Flush do Lote**: Um lote é "fechado" e enviado para processamento em duas condições:
    - O número de logs no buffer atinge o `currentBatchSize`.
    - O tempo desde o último flush atinge o `currentFlushDelay`.
4.  **Adaptação**: Em intervalos regulares, o `adaptationLoop` é executado:
    - Ele analisa a latência média de envio dos últimos lotes e a taxa de transferência (throughput).
    - Se a latência está acima do alvo (`LatencyThreshold`), ele **reduz** o tamanho do lote e o delay para enviar os dados mais rapidamente.
    - Se a latência está baixa e o throughput está abaixo do alvo (`ThroughputTarget`), ele **aumenta** o tamanho do lote para otimizar o envio.

## Papel e Importância

O módulo `batching` é **crucial para a eficiência e a performance** do `log_capturer_go`. Enviar logs um a um é ineficiente e sobrecarrega tanto a rede quanto os sistemas de destino. Agrupar logs em lotes reduz drasticamente o número de requisições e melhora a taxa de transferência.

O aspecto **adaptativo** é particularmente importante porque permite que o sistema se ajuste automaticamente às condições da rede e dos `sinks`:

- **Em momentos de alta carga ou lentidão dos sinks**, ele reduz o tamanho dos lotes para evitar timeouts e grandes reenvios.
- **Em momentos de baixa carga e sinks rápidos**, ele aumenta o tamanho dos lotes para maximizar a eficiência e o throughput.

## Configurações Aplicáveis

As configurações para o `AdaptiveBatcher` são definidas na seção `adaptive_batching` do `config.yaml` (geralmente dentro da configuração de um sink, como o `loki`):

- **`enabled`**: Habilita ou desabilita o batching adaptativo.
- **`min_batch_size` / `max_batch_size`**: Os limites mínimo e máximo para o tamanho do lote.
- **`initial_batch_size`**: O tamanho do lote com o qual o sistema começa.
- **`min_flush_delay` / `max_flush_delay`**: Os limites de tempo de espera para o flush.
- **`latency_threshold`**: A latência de envio (em milissegundos) que, se ultrapassada, fará com que o batcher reduza o tamanho dos lotes.
- **`throughput_target`**: A taxa de transferência desejada (em logs por segundo) que o sistema tentará alcançar aumentando o tamanho dos lotes.

## Problemas e Melhorias

### Problemas Potenciais:

- **Ajuste dos Parâmetros**: Encontrar os valores ideais para os limites (min/max) e os alvos de performance pode ser um desafio e exigir experimentação.
- **Latência Adicional**: O próprio ato de agrupar em lotes introduz uma latência (o `flush_delay`). Em cenários que exigem tempo real estrito, isso pode ser um problema.
- **Comportamento Oscilatório**: Se os fatores de adaptação forem muito agressivos, o sistema pode oscilar constantemente entre lotes grandes e pequenos, sem nunca estabilizar.

### Sugestões de Melhorias:

- **Algoritmo de Adaptação Mais Inteligente**: Utilizar algoritmos de controle mais sofisticados (como controladores PID) para ajustar o tamanho do lote de forma mais suave e estável.
- **Considerar o Tamanho em Bytes**: Além do número de logs, o batcher poderia considerar o tamanho total em bytes do lote, o que é mais relevante para os limites de requisição de muitas APIs.
- **Backpressure Integrado**: Integrar o `AdaptiveBatcher` mais diretamente com o módulo de `backpressure`. Se o backpressure estiver alto, o batcher poderia reduzir drasticamente o tamanho dos lotes ou pausar o envio.
- **Métricas de Adaptação**: Expor mais métricas sobre as decisões de adaptação (quantas vezes aumentou/diminuiu, qual o fator de decisão) para facilitar o ajuste fino dos parâmetros.
