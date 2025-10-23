# Módulo Goroutines

## Estrutura e Operação

O módulo `goroutines` (implementado como `GoroutineTracker` no arquivo `leak_detector.go`) é uma ferramenta de diagnóstico e monitoramento projetada para detectar vazamentos de goroutines (goroutine leaks) na aplicação.

### Principais Componentes da Estrutura:

- **`GoroutineTracker`**: A estrutura principal que monitora o número de goroutines em execução e rastreia o ciclo de vida de goroutines individuais.
- **`GoroutineConfig`**: Contém as configurações para o tracker, como o intervalo de verificação e os `thresholds` (limiares) para a detecção de leaks.
- **`GoroutineInfo`**: Armazena informações sobre uma goroutine rastreada, como seu nome, hora de início e o stack trace no momento da criação.

### Fluxo de Operação:

1.  **Baseline**: Na inicialização, o `GoroutineTracker` estabelece um `baseline` (linha de base) do número de goroutines em execução normal.
2.  **Monitoramento Periódico**: Em intervalos regulares, o tracker verifica o número atual de goroutines no sistema (`runtime.NumGoroutine()`).
3.  **Detecção de Leaks**: A detecção de um possível leak ocorre se:
    - O número total de goroutines excede um `WarnThreshold` ou um `MaxGoroutines`.
    - O aumento no número de goroutines em relação ao `baseline` excede um `LeakThreshold`.
4.  **Rastreamento Individual (Opcional)**: O módulo oferece um método `Track()` que pode ser usado para encapsular a execução de uma goroutine. Ele retorna uma função que deve ser chamada (usando `defer`) quando a goroutine termina. Isso permite que o tracker identifique goroutines específicas que estão ativas por um tempo anormalmente longo.
5.  **Alertas e Diagnóstico**: Ao detectar um possível leak, o tracker pode:
    - Enviar um alerta para um webhook configurado.
    - Logar os stack traces de todas as goroutines em execução para facilitar a depuração.

## Papel e Importância

Goroutine leaks são um tipo de bug comum e perigoso em aplicações Go. Eles ocorrem quando uma goroutine é iniciada, mas nunca termina, ficando "presa" e consumindo memória e recursos indefinidamente. Com o tempo, milhares de goroutines vazadas podem levar à exaustão de memória e à falha da aplicação.

O módulo `goroutines` é **crucial para a estabilidade de longo prazo** do `log_capturer_go`, atuando como um sistema de alerta precoce para este tipo de problema.

Sua importância está em:

- **Prevenção de Falhas**: Ajuda a identificar e a diagnosticar vazamentos de goroutines antes que eles causem uma falha catastrófica na aplicação.
- **Facilitar a Depuração**: Ao fornecer stack traces no momento em que um leak é detectado, ele economiza um tempo valioso de depuração.
- **Monitoramento de Saúde**: O número de goroutines é um indicador importante da saúde e da carga de uma aplicação Go. Monitorar esse número ajuda a entender o comportamento do sistema.

## Configurações Aplicáveis

As configurações para o `GoroutineTracker` são definidas na seção `goroutine_tracking` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita o monitoramento de goroutines.
- **`check_interval`**: A frequência com que o número de goroutines é verificado.
- **`leak_threshold`**: O número de goroutines acima do `baseline` que irá disparar um alerta de leak.
- **`warn_threshold`** e **`max_goroutines`**: Limiares absolutos que disparam alertas de warning ou critical.
- **`tracking_enabled`**: Habilita o rastreamento individual de goroutines através do método `Track()`.
- **`stack_trace_on_leak`**: Se `true`, o tracker irá logar o stack trace de todas as goroutines quando um leak for detectado.

## Problemas e Melhorias

### Problemas Potenciais:

- **Falsos Positivos**: Picos de carga legítimos podem aumentar temporariamente o número de goroutines, o que pode ser confundido com um leak se os `thresholds` forem muito baixos.
- **Overhead de Rastreamento**: O rastreamento individual de cada goroutine (`tracking_enabled`) pode introduzir um pequeno overhead de performance devido à necessidade de sincronização e armazenamento de metadados.
- **Análise de Stack Traces**: Analisar um dump de stack traces de centenas ou milhares de goroutines pode ser uma tarefa complexa.

### Sugestões de Melhorias:

- **Agrupamento de Stack Traces**: Em vez de apenas logar os stack traces brutos, o tracker poderia processá-los e agrupar goroutines com stack traces idênticos. Isso facilitaria a identificação da fonte exata do leak.
- **Baseline Adaptativo**: O `baseline` poderia ser recalculado periodicamente para se adaptar a mudanças no comportamento normal da aplicação ao longo do tempo.
- **Integração com Profiling (pprof)**: Integrar o tracker com as ferramentas de profiling do Go (`pprof`) para permitir a geração de um perfil de goroutines sob demanda quando um leak for detectado.
- **Visualização**: Criar uma página no servidor HTTP da aplicação que visualize o histórico do número de goroutines e liste as goroutines suspeitas de vazamento, juntamente com seus stack traces.
