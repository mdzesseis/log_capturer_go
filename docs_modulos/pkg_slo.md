# Módulo: pkg/slo

## Estrutura

*   `slo.go`: Este arquivo contém o `SLOManager`, que é responsável por monitorar os Objetivos de Nível de Serviço (SLOs) e os Indicadores de Nível de Serviço (SLIs).

## Como funciona

O módulo `pkg/slo` fornece uma estrutura para definir e monitorar SLOs com base em métricas do Prometheus.

1.  **Inicialização (`NewSLOManager`):**
    *   Cria uma nova instância de `SLOManager`.
    *   Inicializa um cliente Prometheus para consultar as métricas de SLI.
    *   Inicializa um `AlertManager` para enviar alertas quando os SLOs são violados.
    *   Adiciona os SLOs definidos na configuração ao gerenciador.

2.  **Avaliação de SLO/SLI (`evaluationLoop`, `evaluateSLO`, `evaluateSLI`):**
    *   O método `Start` inicia uma goroutine em segundo plano que executa o `evaluationLoop`.
    *   Este loop chama periodicamente `evaluateAll` para avaliar todos os SLOs configurados.
    *   Para cada SLO, ele avalia os SLIs associados, consultando o Prometheus para as métricas correspondentes.
    *   Em seguida, ele calcula a conformidade do SLO com base nos valores de SLI e atualiza o orçamento de erro.

3.  **Orçamento de Erro:**
    *   Cada SLO tem um orçamento de erro, que é a quantidade de tempo que o serviço pode ficar fora de conformidade com o SLO sem violá-lo.
    *   A função `updateErrorBudget` atualiza o orçamento de erro com base na conformidade atual do SLO.

4.  **Alerta (`AlertManager`):**
    *   Se um SLO for violado, o `AlertManager` é usado para enviar um alerta para um webhook configurado.

## Papel e Importância

O módulo `pkg/slo` é um componente chave para garantir que a aplicação `log_capturer_go` esteja atingindo suas metas de desempenho e confiabilidade. Seus principais papéis são:

*   **Monitoramento de Desempenho:** Fornece uma maneira de definir e monitorar os principais indicadores de desempenho (KPIs) para a aplicação.
*   **Engenharia de Confiabilidade:** Ajuda a garantir que a aplicação esteja atingindo suas metas de confiabilidade, fornecendo uma estrutura para definir e monitorar SLOs.
*   **Alerta:** Pode alertar automaticamente os operadores quando a aplicação não está atingindo seus SLOs, permitindo que eles tomem medidas corretivas.

## Configurações

O módulo `slo` é configurado através da seção `slo_monitoring` do arquivo `config.yaml`. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita o monitoramento de SLO.
*   `prometheus_url`: A URL do servidor Prometheus para consultar as métricas de SLI.
*   `evaluation_interval`: Com que frequência avaliar os SLOs.
*   `slos`: Uma lista de SLOs a serem monitorados, cada um com seu próprio conjunto de SLIs, orçamento de erro e outros parâmetros.

## Problemas e Melhorias

*   **Análise de Consulta do Prometheus:** A função `parsePrometheusResult` é um placeholder simplificado. Uma implementação mais robusta é necessária para lidar com os diferentes tipos de resultados que podem ser retornados pelas consultas do Prometheus.
*   **Cálculo do Orçamento de Erro:** O cálculo do orçamento de erro também é simplificado. Uma implementação mais sofisticada poderia levar em conta a janela de tempo do SLO e o intervalo de avaliação para fornecer um cálculo mais preciso.
*   **Integração com Dashboard:** O módulo poderia ser aprimorado para gerar automaticamente um dashboard do Grafana para visualizar os SLOs e SLIs.
