# Módulo: pkg/leakdetection

## Estrutura

*   `resource_monitor.go`: Este arquivo contém o `ResourceMonitor`, que é responsável por monitorar os recursos do sistema, como descritores de arquivos, goroutines e memória, para detectar possíveis vazamentos.

## Como funciona

O módulo `pkg/leakdetection` fornece um mecanismo para monitorar o uso de recursos da aplicação e detectar possíveis vazamentos.

1.  **Inicialização (`NewResourceMonitor`):**
    *   Cria uma nova instância de `ResourceMonitor`.
    *   Define valores padrão para o intervalo de monitoramento e os limiares de vazamento.

2.  **Loop de Monitoramento (`monitoringLoop`):**
    *   O método `Start` inicia uma goroutine em segundo plano que executa o `monitoringLoop`.
    *   Este loop chama periodicamente `performCheck` no intervalo definido na configuração.

3.  **Verificação de Recursos (`performCheck`):**
    *   Esta função obtém o uso atual de recursos, incluindo:
        *   O número de descritores de arquivos abertos.
        *   O número de goroutines em execução.
        *   O uso de memória atual.
    *   Em seguida, chama `checkForLeaks` para verificar se algum dos limiares de vazamento configurados foi excedido.

4.  **Detecção de Vazamentos (`checkForLeaks`):**
    *   Esta função compara o uso atual de recursos com os valores de linha de base iniciais.
    *   Se o aumento no uso de recursos exceder um limiar configurado, ele chama `reportLeak` para registrar um aviso ou um erro.
    *   Se a opção `EnableMemoryProfiling` estiver habilitada, ele também registrará os rastreamentos de pilha de todas as goroutines em execução para ajudar na depuração.

5.  **Otimização de GC (`gcOptimizationLoop`):**
    *   Se `EnableGCOptimization` estiver habilitado, uma goroutine em segundo plano é iniciada para verificar periodicamente a taxa de crescimento do heap.
    *   Se o heap estiver crescendo muito rapidamente, ele aciona uma coleta de lixo manual para tentar liberar memória.

## Papel e Importância

O módulo `pkg/leakdetection` é uma ferramenta crucial para garantir a estabilidade e o desempenho a longo prazo da aplicação `log_capturer_go`. Seus principais papéis são:

*   **Detecção de Vazamentos:** Ajuda a detectar tipos comuns de vazamentos de recursos em aplicações Go, como vazamentos de goroutines e de descritores de arquivos.
*   **Monitoramento de Desempenho:** Ao monitorar o uso de recursos ao longo do tempo, pode ajudar a identificar gargalos de desempenho e áreas para otimização.
*   **Depuração:** Quando um vazamento é detectado, as informações fornecidas por este módulo podem ser inestimáveis para depurar o problema.
*   **Estabilidade:** Ao detectar e alertar sobre vazamentos de recursos, pode ajudar a evitar que a aplicação falhe ou se torne irresponsiva devido ao esgotamento de recursos.

## Configurações

A seção `leak_detection` do arquivo `config.yaml` é usada para configurar este módulo. As principais configurações incluem:

*   `monitoring_interval`: Com que frequência verificar vazamentos de recursos.
*   `fd_leak_threshold`: O número de descritores de arquivos acima da linha de base que acionará um aviso de detecção de vazamento.
*   `goroutine_leak_threshold`: O número de goroutines acima da linha de base que acionará um aviso de detecção de vazamento.
*   `memory_leak_threshold`: A quantidade de uso de memória acima da linha de base que acionará um aviso de detecção de vazamento.
*   `alert_cooldown`: A quantidade de tempo a esperar antes de enviar outro alerta para o mesmo tipo de vazamento.
*   `enable_memory_profiling`: Se deve registrar os rastreamentos de pilha quando um vazamento é detectado.
*   `enable_gc_optimization`: Se deve habilitar o recurso de otimização automática de GC.

## Problemas e Melhorias

*   **Código Específico da Plataforma:** A função `getFileDescriptorCount` é específica da plataforma (ela depende do sistema de arquivos `/proc`, que está disponível apenas no Linux). Uma implementação mais portável seria necessária para suportar outros sistemas operacionais.
*   **Estimativa de Uso de CPU:** A função `estimateCPUUsage` no arquivo `adaptive_throttler.go` (que está relacionado a este módulo) é uma estimativa muito simples. Um método mais preciso para medir o uso da CPU seria benéfico.
*   **Detecção de Vazamentos Mais Sofisticada:** A lógica de detecção de vazamentos atual é baseada em limiares simples. Técnicas mais avançadas, como a análise estatística do uso de recursos ao longo do tempo, poderiam ser usadas para detectar vazamentos com mais precisão e reduzir falsos positivos.
