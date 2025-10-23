# Módulo Leak Detection

## Estrutura e Operação

O módulo `leakdetection` (implementado como `ResourceMonitor`) é uma ferramenta de diagnóstico e segurança que monitora ativamente os recursos do sistema utilizados pela aplicação, como descritores de arquivo (file descriptors), goroutines e memória, para detectar possíveis vazamentos (leaks).

### Principais Componentes da Estrutura:

- **`ResourceMonitor`**: A estrutura principal que executa o monitoramento em background.
- **`ResourceMonitorConfig`**: Contém as configurações do monitor, como o intervalo de verificação e os `thresholds` (limiares) que, se ultrapassados, indicam um possível vazamento.
- **`ResourceStats`**: Armazena as estatísticas coletadas, como o número atual de goroutines, o uso de memória e o número de vazamentos detectados.

### Fluxo de Operação:

1.  **Baseline**: Na inicialização, o `ResourceMonitor` captura um `baseline` (linha de base) do uso de recursos, como o número inicial de goroutines e descritores de arquivo.
2.  **Monitoramento Periódico**: Em um loop executado em background, o monitor coleta as métricas atuais de uso de recursos em intervalos regulares (`monitoring_interval`).
3.  **Comparação e Detecção**: As métricas atuais são comparadas com o `baseline` e com os `thresholds` configurados:
    - **Vazamento de Goroutines**: Verifica se o aumento no número de goroutines desde o início (`initialGoroutines`) ultrapassou o `goroutineLeakThreshold`.
    - **Vazamento de Descritores de Arquivo (FDs)**: Verifica se o aumento no número de FDs ultrapassou o `fdLeakThreshold`.
    - **Vazamento de Memória**: Verifica se o uso de memória (`HeapInuse`) ultrapassou o `memoryLeakThreshold`.
4.  **Alerta e Diagnóstico**: Se um `threshold` é ultrapassado, o monitor:
    - Registra um log de alerta com a severidade apropriada (warning, high, critical).
    - Pode enviar um alerta para um `webhook` configurado.
    - Se configurado (`stack_trace_on_leak`), pode fazer um "dump" dos stack traces de todas as goroutines para ajudar a diagnosticar a origem do vazamento.

## Papel e Importância

Vazamentos de recursos são uma classe de bugs perigosos que podem levar à degradação gradual e, eventualmente, à falha total de uma aplicação de longa duração. O módulo `leakdetection` é **essencial para a estabilidade e a confiabilidade** do `log_capturer_go`.

Sua importância reside em:

- **Prevenção de Falhas**: Atua como um sistema de alerta precoce, notificando os administradores sobre o consumo anormal de recursos antes que isso leve à exaustão de memória ou de descritores de arquivo.
- **Saúde do Sistema**: Fornece visibilidade sobre como a aplicação está utilizando os recursos do sistema ao longo do tempo.
- **Facilitar a Depuração**: Em caso de vazamento de goroutines, a capacidade de fazer um dump dos stack traces é uma ferramenta de diagnóstico extremamente valiosa para encontrar a causa raiz do problema.

## Configurações Aplicáveis

As configurações para o `ResourceMonitor` são definidas na seção `resource_monitoring` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita o monitoramento de recursos.
- **`check_interval`**: A frequência com que os recursos do sistema são verificados.
- **`goroutine_threshold`**: O número de goroutines acima do `baseline` que é considerado um possível vazamento.
- **`memory_threshold_mb`**: O limite de uso de memória (em MB) que dispara um alerta.
- **`fd_threshold`**: O número de descritores de arquivo acima do `baseline` que dispara um alerta.
- **`alert_webhook_url`**: A URL de um webhook para onde os alertas de vazamento serão enviados.

## Problemas e Melhorias

### Problemas Potenciais:

- **Falsos Positivos**: Picos de carga normais podem causar um aumento temporário no uso de recursos, o que pode ser interpretado como um vazamento se os `thresholds` forem muito sensíveis.
- **Overhead do Monitoramento**: A verificação constante de recursos, especialmente a contagem de descritores de arquivo e o dump de stack traces, pode introduzir um pequeno overhead de performance.
- **Precisão das Métricas**: A contagem de descritores de arquivo, em particular, depende de mecanismos específicos do sistema operacional (como o `/proc` no Linux) e pode não ser portável ou precisa em todos os ambientes.

### Sugestões de Melhorias:

- **Baseline Dinâmico**: Em vez de um `baseline` estático definido na inicialização, o monitor poderia usar uma média móvel ou um `baseline` que se adapte ao longo do tempo para reduzir falsos positivos.
- **Análise de Tendências**: Implementar uma análise de tendências mais sofisticada. Em vez de apenas comparar com um `threshold` fixo, o monitor poderia detectar um crescimento lento e constante no uso de recursos, que é um forte indicador de um vazamento.
- **Integração com `pprof`**: Integrar o monitor com o `pprof` para que, ao detectar um vazamento, ele possa acionar a coleta de um perfil de heap ou de goroutines, que pode ser analisado posteriormente com as ferramentas do Go.
- **Contexto de Alertas**: Enriquecer os alertas com mais contexto, como as goroutines mais antigas ou os tipos de objetos que mais estão consumindo memória no momento da detecção do leak.
