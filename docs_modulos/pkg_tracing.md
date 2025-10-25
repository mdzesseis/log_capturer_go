# Módulo: pkg/tracing

## Estrutura

*   `tracing.go`: Este arquivo contém o `TracingManager`, que é responsável por gerenciar o rastreamento distribuído na aplicação.

## Como funciona

O módulo `pkg/tracing` fornece uma estrutura para rastreamento distribuído usando o padrão OpenTelemetry.

1.  **Inicialização (`NewTracingManager`):**
    *   Cria uma nova instância de `TracingManager`.
    *   Inicializa um provedor de rastreamento OpenTelemetry com um exportador configurado (por exemplo, Jaeger, OTLP).
    *   Define o provedor de rastreamento global e o propagador para a aplicação.

2.  **Exportadores (`createExporter`):**
    *   A função `createExporter` cria um exportador de rastreamento com base na configuração.
    *   Ele suporta múltiplos exportadores, incluindo Jaeger e OTLP (OpenTelemetry Protocol).

3.  **Contexto Rastreável (`TraceableContext`):**
    *   A struct `TraceableContext` fornece um invólucro em torno do `context.Context` padrão que inclui utilitários de rastreamento.
    *   Ele permite a criação de spans filhos, a adição de atributos e eventos a spans e o registro de erros.

4.  **Funções Instrumentadas (`InstrumentedFunction`):**
    *   A struct `InstrumentedFunction` fornece uma maneira de envolver uma função com rastreamento.
    *   O método `Execute` inicia um novo span antes de executar a função e encerra o span quando a função retorna.

5.  **Middleware HTTP (`TraceHandler`):**
    *   A função `TraceHandler` fornece um middleware HTTP que pode ser usado para rastrear as solicitações HTTP recebidas.
    *   Ele extrai o contexto de rastreamento dos cabeçalhos da solicitação, cria um novo span e injeta o contexto de rastreamento nos cabeçalhos da resposta.

## Papel e Importância

O módulo `pkg/tracing` é um componente crucial para a observabilidade na aplicação `log_capturer_go`. Seus principais papéis são:

*   **Rastreamento Distribuído:** Permite rastrear as solicitações à medida que fluem pelos diferentes componentes da aplicação, o que é essencial para entender o comportamento da aplicação e depurar problemas de desempenho.
*   **Análise de Desempenho:** Os rastreamentos podem ser usados para identificar gargalos de desempenho e áreas para otimização.
*   **Análise da Causa Raiz:** Quando ocorre um erro, os rastreamentos podem ser usados para identificar rapidamente a causa raiz do erro.

## Configurações

O módulo `tracing` é configurado através da seção `tracing` do arquivo `config.yaml`. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita o rastreamento distribuído.
*   `service_name`: O nome do serviço a ser usado para rastreamento.
*   `exporter`: O exportador de rastreamento a ser usado (`jaeger`, `otlp`, etc.).
*   `endpoint`: O endpoint do coletor de rastreamento.
*   `sample_rate`: A taxa de amostragem para rastreamentos.

## Problemas e Melhorias

*   **Propagação de Rastreamento:** O módulo fornece uma struct `TraceableDispatcher`, mas a implementação não está completa. Uma implementação mais completa seria necessária para propagar o contexto de rastreamento através dos diferentes componentes da aplicação.
*   **Integração com Registro:** A função `InjectTraceToLogEntry` fornece uma maneira de injetar informações de rastreamento nas entradas de log. Isso poderia ser mais integrado com a estrutura de registro para injetar automaticamente o contexto de rastreamento em todas as entradas de log.
*   **Instrumentação Automática:** A implementação atual requer a instrumentação manual de funções e manipuladores HTTP. Uma implementação mais avançada poderia usar geração de código ou outras técnicas para instrumentar automaticamente a aplicação.
