# Módulo: pkg/dlq

## Estrutura

*   `dead_letter_queue.go`: Este arquivo contém o componente `DeadLetterQueue`, que é responsável por lidar com entradas de log que falham no processamento.
*   `dead_letter_queue_test.go`: Contém testes unitários para a `DeadLetterQueue`.

## Como funciona

O módulo `pkg/dlq` fornece uma Fila de Mensagens Mortas (Dead Letter Queue - DLQ) para armazenar e gerenciar entradas de log que não puderam ser processadas e enviadas com sucesso para um coletor.

1.  **Inicialização (`NewDeadLetterQueue`):**
    *   Cria uma nova instância de `DeadLetterQueue`.
    *   Define valores padrão para o tamanho da fila, tamanho do arquivo, retenção e outros parâmetros de configuração.
    *   Inicializa uma fila na memória para reter temporariamente as entradas de log com falha antes de serem gravadas no disco.
    *   Se o alerta estiver habilitado, ele inicializa um `AlertManager` para enviar notificações sobre o status da DLQ.

2.  **Adicionando Entradas (`AddEntry`):**
    *   Quando um componente (por exemplo, um coletor) falha ao processar uma entrada de log, ele pode chamar a função `AddEntry`.
    *   Esta função cria uma struct `DLQEntry`, que contém a entrada de log original, a mensagem de erro e outro contexto.
    *   A `DLQEntry` é então adicionada à fila na memória.

3.  **Escrevendo no Disco (`processingLoop` e `writeEntry`):**
    *   Uma goroutine em segundo plano (`processingLoop`) lê as entradas da fila na memória e as grava em um arquivo no disco.
    *   As entradas podem ser gravadas em formato JSON ou em um formato de texto simples.
    *   Os arquivos da DLQ são rotacionados com base em seu tamanho.

4.  **Reprocessamento (`reprocessingLoop`):**
    *   Se o reprocessamento estiver habilitado, outra goroutine em segundo plano (`reprocessingLoop`) lê periodicamente as entradas dos arquivos da DLQ e tenta reprocessá-las.
    *   Ele usa uma função de callback (`reprocessCallback`) para enviar a entrada de volta ao `Dispatcher`.
    *   Se o reprocessamento for bem-sucedido, a entrada é removida da DLQ. Se falhar, a contagem de novas tentativas é incrementada e a entrada será tentada novamente mais tarde com um backoff exponencial.

5.  **Alerta (`AlertManager`):**
    *   O `AlertManager` verifica periodicamente o status da DLQ e pode acionar alertas se:
        *   A taxa de novas entradas for muito alta.
        *   O número total de entradas exceder um limiar.
        *   A fila na memória estiver quase cheia.

## Papel e Importância

O módulo `pkg/dlq` é um componente crítico para garantir a confiabilidade e a integridade dos dados da aplicação `log_capturer_go`. Seus principais papéis são:

*   **Prevenção de Perda de Dados:** Fornece uma rede de segurança para entradas de log que falham no processamento, evitando que sejam perdidas.
*   **Solução de Problemas:** O conteúdo da DLQ pode ser inestimável para depurar problemas com coletores ou pipelines de processamento.
*   **Recuperação Automática:** O mecanismo de reprocessamento permite que o sistema se recupere automaticamente de falhas transitórias, como uma interrupção temporária da rede.
*   **Alerta:** O mecanismo de alerta pode notificar os operadores sobre problemas com o pipeline de processamento de logs, permitindo que eles tomem medidas corretivas.

## Configurações

O módulo `dlq` é configurado através da seção `dispatcher.dlq_config` do arquivo `config.yaml`. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita a DLQ.
*   `directory`: O diretório onde os arquivos da DLQ são armazenados.
*   `max_file_size_mb`: O tamanho máximo de um único arquivo da DLQ.
*   `retention_days`: Por quanto tempo manter os arquivos da DLQ antes de excluí-los.
*   **Seção `reprocessing_config`:**
    *   `enabled`: Habilita ou desabilita o reprocessamento automático.
    *   `interval`: Com que frequência tentar o reprocessamento.
    *   `max_retries`: O número máximo de vezes para tentar reprocessar uma entrada.
*   **Seção `alert_config`:**
    *   `enabled`: Habilita ou desabilita o alerta da DLQ.
    *   `webhook_url`: A URL de um webhook para enviar alertas.

## Problemas e Melhorias

*   **Estratégia de Reprocessamento:** A estratégia de reprocessamento atual é enviar a entrada com falha de volta para o `Dispatcher`. Uma estratégia mais sofisticada poderia ser tentar enviar a entrada para um coletor diferente ou aplicar um pipeline de processamento diferente.
*   **Formato de Dados da DLQ:** As entradas da DLQ são armazenadas em formato JSON ou em um formato de texto simples. Um formato mais estruturado и eficiente, como Protocol Buffers, poderia ser usado para reduzir a pegada de armazenamento e melhorar o desempenho de leitura e escrita na DLQ.
*   **Reprocessamento Manual:** A API para reprocessar manualmente as entradas da DLQ não está totalmente implementada. Este seria um recurso útil para os operadores terem.
