# Módulo: pkg/batching

## Estrutura

*   `adaptive_batcher.go`: Este arquivo contém o `AdaptiveBatcher`, que implementa uma estratégia de loteamento inteligente com dimensionamento dinâmico.

## Como funciona

O módulo `pkg/batching` fornece um mecanismo de loteamento sofisticado que se adapta à carga e ao desempenho atuais do sistema.

1.  **Inicialização (`NewAdaptiveBatcher`):**
    *   Cria uma nova instância do `AdaptiveBatcher`.
    *   Define valores padrão para os tamanhos mínimo e máximo dos lotes, atrasos de descarga e outros parâmetros de configuração.
    *   Inicializa um canal (`flushChan`) para enviar lotes ao consumidor (por exemplo, o `Dispatcher`).

2.  **Adicionando Entradas (`Add`):**
    *   Quando uma nova entrada de log é adicionada, ela é anexada ao lote atual.
    *   Se o tamanho do lote atingir o `currentBatchSize` atual, o lote é descarregado.
    *   Um temporizador de descarga é redefinido toda vez que uma nova entrada é adicionada. Se o temporizador expirar, o lote é descarregado mesmo que não esteja cheio.

3.  **Adaptação (`adaptationLoop` e `adaptParameters`):**
    *   O `AdaptiveBatcher` executa periodicamente um loop de adaptação para ajustar o tamanho do lote e o atraso de descarga.
    *   Ele calcula a latência média do processamento de lotes e o throughput atual.
    *   **Se a latência for muito alta:** Ele reduz o tamanho do lote e o atraso de descarga para enviar lotes menores com mais frequência.
    *   **Se o throughput for baixo e a latência for aceitável:** Ele aumenta o tamanho do lote e o atraso de descarga para enviar lotes maiores e menos frequentes.
    *   Isso permite que o loteador encontre o equilíbrio ideal entre throughput e latência para as condições atuais do sistema.

4.  **Contrapressão (Backpressure):**
    *   Se o `flushChan` estiver cheio (o que significa que o consumidor não está processando os lotes rápido o suficiente), o `AdaptiveBatcher` descartará o lote atual e registrará um aviso. Isso impede que o loteador consuma uma quantidade ilimitada de memória.

## Papel e Importância

O módulo `pkg/batching` é um componente chave para otimizar o desempenho e a eficiência do pipeline de processamento de logs. Seus principais papéis são:

*   **Otimização de Desempenho:** Ao agrupar entradas de log em lotes, ele reduz a sobrecarga de enviar logs individuais para os coletores, especialmente para coletores remotos que têm uma alta latência por solicitação.
*   **Desempenho Adaptativo:** A natureza adaptativa do loteador permite que ele ajuste automaticamente seu desempenho com base na carga atual do sistema e nas condições da rede, sem a necessidade de intervenção manual.
*   **Gerenciamento de Recursos:** Ajuda a gerenciar o uso da memória, controlando o tamanho dos lotes na memória e aplicando contrapressão quando o consumidor está sobrecarregado.

## Configurações

A seção `adaptive_batching` do arquivo `config.yaml` (dentro da configuração de um coletor) é usada para configurar este módulo. As principais configurações incluem:

*   `min_batch_size`, `max_batch_size`: Os tamanhos mínimo e máximo de lote permitidos.
*   `min_flush_delay`, `max_flush_delay`: Os atrasos de descarga mínimo e máximo permitidos.
*   `latency_threshold`: O limiar de latência que aciona uma redução no tamanho do lote.
*   `throughput_target`: O throughput alvo que o loteador tentará alcançar.
*   `adaptation_interval`: Com que frequência executar a lógica de adaptação.

## Problemas e Melhorias

*   **Estratégia de Contrapressão:** A estratégia de contrapressão atual é descartar lotes quando o `flushChan` está cheio. Uma estratégia mais sofisticada poderia ser implementar uma fila de vários níveis ou enviar os lotes descartados para uma Fila de Mensagens Mortas (Dead Letter Queue).
*   **Lógica de Adaptação Mais Sofisticada:** A lógica de adaptação atual é baseada em heurísticas simples. Algoritmos mais avançados, como um controlador PID, poderiam ser usados para um controle mais preciso sobre o tamanho do lote e o atraso de descarga.
*   **Adaptação por Coletor:** O `AdaptiveBatcher` é atualmente configurado por coletor. Uma estratégia de loteamento mais global que leve em consideração o estado de todos os coletores poderia ser benéfica.
