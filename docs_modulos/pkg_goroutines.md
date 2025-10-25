# Módulo: pkg/goroutines

## Estrutura

*   `leak_detector.go`: Este arquivo contém o `GoroutineTracker`, que é responsável por monitorar o número de goroutines na aplicação e detectar possíveis vazamentos.

## Como funciona

O módulo `pkg/goroutines` fornece um mecanismo para rastrear o ciclo de vida das goroutines e detectar possíveis vazamentos.

1.  **Inicialização (`NewGoroutineTracker`):**
    *   Cria uma nova instância de `GoroutineTracker`.
    *   Registra o número inicial de goroutines como uma linha de base.

2.  **Loop de Monitoramento (`monitoringLoop`):**
    *   O método `Start` inicia uma goroutine em segundo plano que executa o `monitoringLoop`.
    *   Este loop chama periodicamente `checkForLeaks` no intervalo definido na configuração.

3.  **Detecção de Vazamentos (`checkForLeaks`):**
    *   Esta função obtém o número atual de goroutines e o compara com a linha de base.
    *   Se o número de goroutines exceder um limiar configurado, ele registra um aviso ou um erro.
    *   Se a opção `StackTraceOnLeak` estiver habilitada, ele também despejará os rastreamentos de pilha de todas as goroutines em execução para ajudar na depuração.

4.  **Rastreamento de Goroutines (`Track`):**
    *   A função `Track` pode ser usada para rastrear explicitamente o ciclo de vida de uma goroutine.
    *   Ela retorna uma função que deve ser chamada quando a goroutine termina.
    *   Isso permite que o `GoroutineTracker` identifique quais goroutines específicas ainda estão em execução e por quanto tempo, o que pode ser muito útil para identificar a origem de um vazamento.

## Papel e Importância

O módulo `pkg/goroutines` é uma ferramenta importante para garantir a estabilidade e o desempenho da aplicação `log_capturer_go`. Seus principais papéis são:

*   **Detecção de Vazamentos:** Vazamentos de goroutines são um problema comum em aplicações Go e podem levar a um consumo excessivo de memória e degradação do desempenho. Este módulo ajuda a detectar esses vazamentos precocemente.
*   **Depuração:** Quando um vazamento é detectado, as informações fornecidas por este módulo (como o número de goroutines em execução e seus rastreamentos de pilha) podem ser inestimáveis para depurar o problema.
*   **Monitoramento de Desempenho:** Ao monitorar o número de goroutines, este módulo pode ajudar a identificar gargalos de desempenho e áreas para otimização.

## Configurações

A seção `goroutine_tracking` do arquivo `config.yaml` é usada para configurar este módulo. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita o rastreamento de goroutines.
*   `check_interval`: Com que frequência verificar vazamentos.
*   `leak_threshold`: O número de goroutines acima da linha de base que acionará um aviso de detecção de vazamento.
*   `max_goroutines`: O número máximo de goroutines permitido antes que um alerta crítico seja acionado.
*   `stack_trace_on_leak`: Se deve despejar os rastreamentos de pilha quando um vazamento é detectado.

## Problemas e Melhorias

*   **Gerenciamento da Linha de Base:** O número da linha de base de goroutines é definido na inicialização. Em uma aplicação de longa duração, o número "normal" de goroutines pode mudar com o tempo. O módulo poderia ser aprimorado para ajustar dinamicamente a linha de base.
*   **Análise da Causa Raiz:** Embora o módulo possa identificar que um vazamento está ocorrendo, ainda pode ser desafiador identificar a causa raiz exata. Uma análise mais avançada dos rastreamentos de pilha poderia ser implementada para ajudar com isso.
*   **Integração com Ferramentas de Profiling:** O módulo poderia ser integrado com as ferramentas de profiling nativas do Go (por exemplo, `pprof`) para fornecer informações ainda mais detalhadas sobre o comportamento das goroutines.
