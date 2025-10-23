# Módulo Sinks

## Estrutura e Operação

O módulo `sinks` é responsável por enviar os logs processados para seus destinos finais. Ele é projetado com uma arquitetura de plugins, onde cada "sink" é uma implementação que sabe como se comunicar com um sistema de armazenamento ou análise de logs específico.

### Principais Componentes da Estrutura:

- **Interface `Sink`**: Define um contrato comum que todos os sinks devem seguir. Inclui métodos como `Start()`, `Stop()` e `Send()`, garantindo que o `dispatcher` possa interagir com qualquer sink de maneira uniforme.
- **Implementações de Sinks**: O módulo contém implementações para diferentes backends:
    - **`LokiSink`**: Envia logs para o Grafana Loki.
    - **`LocalFileSink`**: Salva os logs em arquivos no sistema de arquivos local, com suporte a rotação e compressão.
    - **`ElasticsearchSink`**: Envia logs para um cluster Elasticsearch.
    - **`SplunkSink`**: Envia logs para o Splunk através do HTTP Event Collector (HEC).
- **`common.go`**: Fornece funcionalidades compartilhadas entre os diferentes sinks, como a configuração de TLS.

### Fluxo de Operação:

1.  **Inicialização**: Os sinks são inicializados pelo módulo `app` com base na seção `sinks` do arquivo `config.yaml`.
2.  **Registro no Dispatcher**: Cada sink inicializado é registrado no `dispatcher`.
3.  **Recebimento de Lotes**: O `dispatcher` envia lotes de logs para o método `Send()` de cada sink configurado.
4.  **Envio para o Destino**: Cada sink é responsável por formatar os dados da maneira esperada pelo seu backend e por gerenciar a comunicação (ex: requisições HTTP, escrita em arquivo).
5.  **Gerenciamento de Conexão e Falhas**: Os sinks implementam sua própria lógica de batching, retries, e em alguns casos, circuit breakers para lidar com falhas de comunicação com o backend.

## Papel e Importância

O módulo `sinks` é a **ponte entre o `log_capturer_go` e o ecossistema de observabilidade externo**. Ele é o responsável final por garantir que os logs cheguem ao seu destino.

Sua importância está na:

- **Extensibilidade**: A arquitetura baseada em interface facilita a adição de novos sinks para suportar outros backends (como OpenSearch, Datadog, etc.) sem a necessidade de alterar o núcleo da aplicação.
- **Resiliência**: Cada sink implementa sua própria lógica de tratamento de falhas, como retries e circuit breakers, tornando o envio de dados mais robusto.
- **Eficiência**: Os sinks são projetados para enviar dados em lote e utilizar compressão, otimizando o uso de rede e a performance dos backends.

## Configurações Aplicáveis

As configurações para cada sink são definidas na seção `sinks` do `config.yaml`. Cada tipo de sink tem suas próprias configurações específicas:

### LokiSink:
- **`enabled`**: Habilita o sink.
- **`url`**: A URL da API de push do Loki.
- **`tenant_id`**: ID do tenant para ambientes Loki multi-tenant.
- **`batch_size`** e **`batch_timeout`**: Configurações de lote para o envio.

### LocalFileSink:
- **`enabled`**: Habilita o sink.
- **`directory`**: O diretório onde os arquivos de log serão salvos.
- **`filename`**: O padrão para os nomes dos arquivos.
- **`rotation`**: Configurações para a rotação de arquivos (tamanho máximo, número de arquivos, etc.).

### ElasticsearchSink e SplunkSink:
- Configurações similares para URL, autenticação (tokens, senhas), nomes de índices e políticas de lote.

## Problemas e Melhorias

### Problemas Potenciais:

- **Formato de Dados**: Cada backend de log tem seu próprio formato de dados e API. Manter a compatibilidade e a performance para cada um pode ser um desafio.
- **Autenticação**: Gerenciar diferentes métodos de autenticação (API keys, basic auth, etc.) para cada sink pode ser complexo.
- **Backpressure**: Se um sink estiver lento ou indisponível, ele pode causar o acúmulo de logs na fila do `dispatcher`, consumindo memória.

### Sugestões de Melhorias:

- **Buffer em Disco por Sink**: Implementar um buffer em disco individual para cada sink. Se um sink específico ficar indisponível, os logs para ele seriam persistidos em disco em vez de bloquear o `dispatcher`.
- **Health Checks Ativos**: Implementar health checks mais ativos para cada sink, permitindo que o `dispatcher` pare de enviar dados para um sink que está comprovadamente fora do ar.
- **Suporte a Mais Sinks**: Adicionar suporte nativo a outros backends populares, como Datadog, New Relic, ou S3.
- **Templates de Payload**: Permitir que os usuários personalizem o formato do payload enviado para os sinks através de templates, oferecendo mais flexibilidade.
