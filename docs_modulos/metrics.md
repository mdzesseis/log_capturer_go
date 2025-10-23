# Módulo Metrics

## Estrutura e Operação

O módulo `metrics` é responsável por coletar e expor métricas da aplicação no formato Prometheus, permitindo o monitoramento e a observabilidade do sistema.

### Principais Componentes da Estrutura:

- **Métricas Globais**: O módulo define uma série de métricas padrão do Prometheus (Counters, Gauges, Histograms) que são usadas em toda a aplicação para rastrear eventos importantes, como o número de logs processados, taxas de erro, utilização de filas e latências.
- **`MetricsServer`**: Uma estrutura que encapsula um servidor HTTP dedicado a expor as métricas no endpoint `/metrics`.
- **`NewMetricsServer(...)`**: Função que cria e registra todas as métricas padrão e inicia o servidor HTTP.
- **Funções Auxiliares**: O módulo fornece um conjunto de funções auxiliares (ex: `RecordLogProcessed`, `RecordError`) que facilitam a instrumentação do código e a atualização das métricas de forma consistente.
- **`EnhancedMetrics`**: Uma estrutura adicional que oferece a capacidade de coletar métricas mais detalhadas e avançadas, como estatísticas de disco, rede e performance de componentes específicos.

### Fluxo de Operação:

1.  **Inicialização**: O `MetricsServer` é inicializado pelo módulo `app`, que registra todas as métricas e inicia um servidor HTTP em uma porta dedicada.
2.  **Coleta**: Ao longo da execução da aplicação, diferentes componentes invocam as funções auxiliares do módulo `metrics` para registrar eventos (ex: um log processado, um erro ocorrido).
3.  **Exposição**: O servidor de métricas expõe as métricas coletadas no endpoint `/metrics`. Um sistema de monitoramento como o Prometheus pode então fazer o "scrape" (coleta) desses dados periodicamente.
4.  **Métricas do Sistema**: O `EnhancedMetrics` (se habilitado) coleta métricas do sistema (uso de CPU, memória, goroutines) em intervalos regulares e as atualiza.

## Papel e Importância

O módulo `metrics` é **vital para a observabilidade e o monitoramento** da saúde e da performance do `log_capturer_go`. Sem ele, seria muito difícil entender o que está acontecendo dentro da aplicação em um ambiente de produção.

Sua importância se manifesta em:

- **Visibilidade**: Fornece uma visão detalhada do comportamento da aplicação, como a taxa de processamento de logs, a latência dos sinks e a utilização de recursos.
- **Alertas**: As métricas expostas podem ser usadas para configurar alertas proativos (ex: alertar se a taxa de erro aumentar ou se uma fila estiver quase cheia).
- **Análise de Performance**: Os histogramas de latência e os gauges de throughput são essenciais para identificar gargalos e otimizar a performance.
- **Planejamento de Capacidade**: As métricas de uso de recursos ajudam a entender como a aplicação se comporta sob carga e a planejar a alocação de recursos.

## Configurações Aplicáveis

As configurações para o módulo de métricas são definidas na seção `metrics` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita o servidor de métricas.
- **`port`**: A porta em que o servidor de métricas será exposto (ex: 8001).
- **`path`**: O caminho do endpoint de métricas (padrão: `/metrics`).

## Problemas e Melhorias

### Problemas Potenciais:

- **Cardinalidade de Labels**: O uso excessivo de labels com alta cardinalidade (como IDs únicos) pode sobrecarregar o Prometheus e consumir muita memória. É preciso ter cuidado ao definir e usar os labels das métricas.
- **Overhead de Métricas**: A coleta de um número muito grande de métricas pode introduzir um overhead de performance na aplicação.
- **Consistência**: Se as métricas não forem atualizadas de forma consistente e atômica, elas podem apresentar valores incorretos.

### Sugestões de Melhorias:

- **Métricas por Tenant**: Em um ambiente multi-tenant, seria útil adicionar um label de `tenant_id` à maioria das métricas para permitir a visualização e o alerta por tenant.
- **Dashboards Pré-configurados**: Fornecer dashboards Grafana pré-configurados que visualizem as métricas mais importantes do `log_capturer_go` facilitaria o onboarding de novos usuários.
- **Métricas de Negócio**: Além das métricas operacionais, poderiam ser adicionadas métricas de negócio, como o volume de logs por tipo de cliente ou por tipo de aplicação monitorada.
- **Push Gateway**: Para cenários onde o `log_capturer_go` é executado como um job de curta duração, a integração com um Prometheus Push Gateway poderia ser útil.
