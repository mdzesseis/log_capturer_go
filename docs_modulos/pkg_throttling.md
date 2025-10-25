# Módulo: pkg/throttling

## Estrutura

*   `adaptive_throttler.go`: Este arquivo contém o `AdaptiveThrottler`, que implementa um mecanismo de throttling adaptativo com base na carga do sistema.

## Como funciona

O módulo `pkg/throttling` fornece um mecanismo para diminuir a taxa de processamento da aplicação para evitar que ela consuma muitos recursos.

1.  **Inicialização (`NewAdaptiveThrottler`):**
    *   Cria uma nova instância de `AdaptiveThrottler`.
    *   Define valores padrão para o tempo de suspensão inicial, limiares de CPU e memória e outros parâmetros de configuração.
    *   Inicializa um conjunto de `MetricWindow`s para rastrear o histórico recente de uso de CPU, uso de memória e tamanhos de fila.
    *   Inicia uma goroutine em segundo plano (`monitoringLoop`) para coletar periodicamente as métricas do sistema e ajustar o nível de throttling.

2.  **Throttling (`Throttle`, `ThrottleWithQueueSize`):**
    *   O método `Throttle` introduz um atraso (suspensão) para diminuir a taxa de processamento.
    *   A duração do atraso é determinada pelo `currentSleepBase`, que é ajustado dinamicamente com base na carga do sistema.
    *   O método `ThrottleWithQueueSize` é semelhante, mas também leva em consideração o tamanho da fila de processamento ao calcular o atraso.

3.  **Adaptação (`monitoringLoop`, `adaptThrottling`):**
    *   O `monitoringLoop` chama periodicamente `collectMetrics` para obter o uso atual de CPU e memória e `adaptThrottling` para ajustar o nível de throttling.
    *   A função `adaptThrottling` calcula uma "pontuação de carga" com base no uso atual de CPU, uso de memória e tamanho da fila.
    *   **Se a pontuação de carga for alta:** Ele aumenta o `currentSleepBase` para aumentar o atraso de throttling.
    *   **Se a pontuação de carga for baixa:** Ele diminui o `currentSleepBase` para reduzir o atraso de throttling.
    *   Isso permite que o throttler ajuste automaticamente a taxa de processamento para corresponder aos recursos do sistema disponíveis.

## Papel e Importância

O módulo `pkg/throttling` é um componente importante para garantir a estabilidade e a confiabilidade da aplicação `log_capturer_go`. Seus principais papéis são:

*   **Autopreservação:** Impede que a aplicação consuma muitos recursos e se torne irresponsiva ou falhe.
*   **Desempenho Adaptativo:** Ajusta automaticamente a taxa de processamento para corresponder aos recursos do sistema disponíveis, o que ajuda a maximizar o throughput sem causar sobrecarga.
*   **Justiça:** Garante que a aplicação não prive outros processos no sistema de recursos.

## Configurações

O módulo `throttling` é configurado através da seção `dispatcher.throttling_config` do arquivo `config.yaml`. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita o throttling.
*   `sleep_base`: O tempo de suspensão inicial.
*   `cpu_low_threshold`, `cpu_high_threshold`: Os limiares de uso de CPU para ajustar o nível de throttling.
*   `memory_low_threshold`, `memory_high_threshold`: Os limiares de uso de memória para ajustar o nível de throttling.
*   `monitoring_interval`: Com que frequência coletar as métricas do sistema e ajustar o nível de throttling.

## Problemas e Melhorias

*   **Estimativa de Uso de CPU:** A função `estimateCPUUsage` é uma estimativa muito simples baseada no número de goroutines. Um método mais preciso para medir o uso da CPU seria benéfico.
*   **Cálculo da Pontuação de Carga:** O cálculo da pontuação de carga é baseado em uma média ponderada simples das métricas de CPU, memória e tamanho da fila. Um algoritmo mais sofisticado poderia ser usado para fornecer uma pontuação de carga mais precisa e responsiva.
*   **Integração com Contrapressão:** O mecanismo de throttling poderia ser mais integrado com o mecanismo de contrapressão para fornecer uma abordagem mais holística para a proteção contra sobrecarga.
