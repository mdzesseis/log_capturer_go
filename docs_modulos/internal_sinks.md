# Módulo: internal/sinks

## Estrutura

*   `common.go`: Contém código comum usado por diferentes coletores, como configuração de TLS e um gerenciador básico de segredos.
*   `loki_sink.go`: Implementa o coletor para enviar logs para o Grafana Loki.
*   `local_file_sink.go`: Implementa o coletor para escrever logs em arquivos locais.
*   `elasticsearch_sink.go`: Implementa o coletor para enviar logs para o Elasticsearch.
*   `splunk_sink.go`: Implementa o coletor para enviar logs para o Splunk.

## Como funciona

O módulo `internal/sinks` é responsável por entregar as entradas de log processadas aos seus destinos finais. Cada implementação de coletor está em conformidade com a interface `types.Sink`, que define os métodos `Start`, `Send` e `Stop`.

#### `loki_sink.go`

1.  **Inicialização (`NewLokiSink`):**
    *   Cria um novo `LokiSink`.
    *   Configura um cliente HTTP com um tempo limite e um pool de conexões.
    *   Configura um disjuntor para evitar sobrecarregar uma instância do Loki com falha.
    *   Inicializa um compressor HTTP para comprimir lotes de logs antes de enviar.
    *   Se habilitado, configura um `AdaptiveBatcher` para ajustar dinamicamente o tamanho do lote e o atraso de descarga com base no desempenho.

2.  **Envio de Logs (`Send` e `sendBatch`):**
    *   O método `Send` recebe entradas de log e as coloca em uma fila interna.
    *   Goroutines de trabalho (`processLoop` ou `adaptiveBatchLoop`) retiram as entradas da fila e as agrupam em lotes.
    *   A função `sendBatch` envia o lote para o Loki. Ele usa o disjuntor para envolver a chamada `sendToLoki`.
    *   A função `sendToLoki` agrupa as entradas de log por fluxo (com base em seus rótulos), cria uma carga útil do Loki, a comprime e a envia para a API de push do Loki.

#### `local_file_sink.go`

1.  **Inicialização (`NewLocalFileSink`):**
    *   Cria um novo `LocalFileSink`.
    *   Configura uma fila para as entradas de log recebidas.
    *   Inicia goroutines em segundo plano para processar a fila, rotacionar arquivos e monitorar o espaço em disco.

2.  **Escrita de Logs (`writeLogEntry`):**
    *   O `processLoop` lê as entradas de log da fila e chama `writeLogEntry`.
    *   Esta função determina o nome correto do arquivo de log com base no timestamp e nos rótulos do log.
    *   Em seguida, abre ou cria o arquivo de log e escreve a entrada de log nele, em formato JSON ou texto.

3.  **Rotação e Limpeza de Arquivos:**
    *   O `rotationLoop` verifica periodicamente se algum arquivo de log excedeu seu tamanho máximo e os rotaciona, se necessário.
    *   O `diskMonitorLoop` verifica periodicamente o uso total do espaço em disco e pode acionar uma limpeza de emergência se o disco estiver ficando cheio.

#### `elasticsearch_sink.go` e `splunk_sink.go`

Esses coletores seguem um padrão semelhante ao `LokiSink`:

*   Eles usam uma fila interna e goroutines de trabalho para processar as entradas de log em lotes.
*   Eles formatam as entradas de log no formato apropriado para seus respectivos back-ends (API em massa do Elasticsearch ou Splunk HEC).
*   Eles usam um cliente HTTP para enviar os lotes.
*   Eles implementam a lógica de nova tentativa com backoff exponencial para lidar com falhas transitórias.

## Papel e Importância

O módulo `sinks` é a etapa final no pipeline de processamento de logs. Sua importância reside em:

*   **Entrega de Dados:** É responsável por entregar logs de forma confiável aos seus destinos finais.
*   **Resiliência:** Recursos como novas tentativas, disjuntores e contrapressão ajudam a garantir que os logs não sejam perdidos, mesmo quando os sistemas downstream estão indisponíveis ou sob carga pesada.
*   **Flexibilidade:** Ao suportar vários tipos de coletores, a aplicação pode ser integrada a uma variedade de sistemas de registro e monitoramento.
*   **Desempenho:** O envio em lote e a compressão ajudam a melhorar a eficiência da entrega de logs, especialmente para coletores remotos.

## Configurações

O módulo `sinks` é configurado através da seção `sinks` do arquivo `config.yaml`. Cada tipo de coletor tem sua própria seção de configuração (por exemplo, `loki`, `local_file`).

## Problemas e Melhorias

*   **Integração com DLQ:** O `LokiSink` tem um campo `deadLetterQueue`, mas os outros coletores não. Uma integração consistente com DLQ em todos os coletores seria benéfica.
*   **Duplicação de Código:** Há alguma duplicação de código entre as diferentes implementações de coletores, especialmente nas áreas de enfileiramento, envio em lote e novas tentativas. Isso poderia ser refatorado em uma implementação base comum.
*   **Gerenciamento de Segredos:** Os coletores atualmente obtêm suas credenciais diretamente da configuração. Isso deve ser melhorado com a integração a um sistema de gerenciamento de segredos adequado.
*   **Mais Coletores:** A aplicação poderia ser estendida para suportar mais tipos de coletores, como Kafka, Fluentd ou outros serviços de registro nativos da nuvem.
