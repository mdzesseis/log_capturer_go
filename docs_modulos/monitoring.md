# Módulo Monitoring

## Estrutura e Operação

O módulo `monitoring` (implementado como `EnhancedMetrics`) é uma camada de observabilidade avançada sobre o módulo `metrics` principal. Enquanto o `metrics` se concentra nas métricas operacionais básicas, o `monitoring` coleta e expõe um conjunto mais rico e detalhado de métricas de performance, de sistema e de negócio.

### Principais Componentes da Estrutura:

- **`EnhancedMetrics`**: A estrutura principal que define e gerencia um vasto conjunto de métricas Prometheus, incluindo:
    - **Métricas de Aplicação**: Taxa de processamento de logs, latência, tamanho de filas, taxas de erro.
    - **Métricas de Sistema**: Uso de CPU e memória, contagem de goroutines e de descritores de arquivo.
    - **Métricas de Performance**: Latência de operações específicas, estatísticas de connection pools, taxa de compressão.
    - **Métricas de Negócio**: Contagem de contêineres e arquivos monitorados, saúde dos `sinks`.
- **`PerformanceProfiler`**: Uma sub-estrutura que permite a criação de "traces" de performance para medir a latência de operações específicas de forma mais granular.

### Fluxo de Operação:

1.  **Inicialização**: O `EnhancedMetrics` é inicializado e registra todos os seus vetores de métricas no Prometheus.
2.  **Coleta de Métricas de Sistema**: Uma goroutine em background (`collectSystemMetrics`) é iniciada para coletar periodicamente métricas do sistema operacional e do runtime do Go (uso de memória, número de goroutines, etc.).
3.  **Instrumentação**: Outros módulos da aplicação invocam os métodos do `EnhancedMetrics` para registrar eventos e atualizar as métricas. Por exemplo:
    - O `dispatcher` chama `RecordProcessingDuration` para registrar a latência de processamento.
    - Os `sinks` chamam `UpdateSinkHealth` para reportar seu estado de saúde.
    - O `leakdetector` chama `UpdateLeakDetection` para registrar métricas sobre vazamentos de recursos.
4.  **Exposição**: Todas as métricas coletadas são automaticamente expostas através do mesmo endpoint `/metrics` gerenciado pelo módulo `metrics` principal, prontas para serem coletadas pelo Prometheus.

## Papel e Importância

O módulo `monitoring` eleva a observabilidade do `log_capturer_go` a um nível profissional. Ele fornece os dados detalhados necessários não apenas para saber *se* a aplicação está funcionando, mas *quão bem* ela está funcionando.

Sua importância reside em:

- **Visibilidade Profunda**: Oferece uma visão muito mais granular da performance de cada componente e do uso de recursos do sistema.
- **Diagnóstico Avançado**: Permite correlacionar problemas de performance com o uso de recursos. Por exemplo, é possível ver se um aumento na latência de processamento está relacionado a um pico no uso de CPU ou a uma contenção em um `connection pool`.
- **Otimização de Performance**: As métricas detalhadas são essenciais para identificar gargalos e otimizar o código da aplicação.
- **Alertas Inteligentes**: Permite a criação de alertas mais sofisticados, baseados em percentis de latência (P95, P99), taxas de compressão ou saúde de `sinks` individuais.

## Configurações Aplicáveis

O módulo `monitoring` em si não possui uma seção de configuração dedicada no `config.yaml`, pois ele é projetado para ser uma camada de instrumentação interna. Sua operação é habilitada implicitamente quando o `metrics` está habilitado. As configurações que o afetam são as mesmas do módulo `metrics`:

- **`metrics.enabled`**: Habilita a coleta e exposição de todas as métricas, incluindo as avançadas.
- **`metrics.port`**: Define a porta do endpoint de métricas.

## Problemas e Melhorias

### Problemas Potenciais:

- **Overhead de Cardinalidade**: Assim como no módulo `metrics`, a alta cardinalidade dos labels é um risco. Métricas como `log_processing_duration` com labels para `pipeline` e `stage` podem gerar muitas séries temporais se houver muitos pipelines e stages.
- **Complexidade de Métricas**: Um número muito grande de métricas pode tornar os dashboards e as consultas no Prometheus complexos e difíceis de gerenciar.
- **Precisão das Métricas de Sistema**: A coleta de métricas como uso de CPU a partir da própria aplicação Go pode ser imprecisa. Ferramentas externas como o `node_exporter` do Prometheus são geralmente mais adequadas para isso.

### Sugestões de Melhorias:

- **Dashboards Pré-Construídos**: Fornecer um conjunto de dashboards Grafana que já venham configurados para visualizar as métricas avançadas deste módulo seria de grande valor para os usuários.
- **Registro de Métricas Customizadas**: A funcionalidade `RegisterCustomMetric` é um bom começo. Ela poderia ser expandida para permitir que os usuários definam e registrem suas próprias métricas através da configuração YAML, sem precisar alterar o código.
- **Cálculo de Percentis no Lado do Servidor**: Atualmente, o cálculo de percentis é feito através de histogramas do Prometheus. Para maior precisão, a aplicação poderia calcular os percentis exatos e expô-los como `Gauges` (embora isso tenha um custo de performance).
- **Exportadores Adicionais**: Além do Prometheus, adicionar suporte para exportar as métricas para outros formatos e sistemas de monitoramento, como InfluxDB, StatsD ou OpenTelemetry Metrics.
