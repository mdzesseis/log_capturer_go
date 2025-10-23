# Módulo Persistence

## Estrutura e Operação

O módulo `persistence` (implementado como `BatchPersistence`) fornece um mecanismo para persistir lotes (batches) de logs em disco antes de serem enviados aos `sinks`. Seu principal objetivo é garantir a durabilidade dos dados, permitindo a recuperação de lotes em caso de falha ou reinicialização da aplicação.

### Principais Componentes da Estrutura:

- **`BatchPersistence`**: A estrutura principal que gerencia a persistência e a recuperação de lotes.
- **`PersistedBatch`**: Representa um lote de logs que foi salvo em disco. Contém os logs, o ID do lote, o `sink` de destino e metadados sobre as tentativas de envio.
- **`Config`**: Contém as configurações para a persistência, como o diretório de armazenamento, o TTL dos lotes e as políticas de `retry` para a recuperação.

### Fluxo de Operação:

1.  **Persistência**: Antes de o `dispatcher` tentar enviar um lote para um `sink`, ele pode primeiro chamar `PersistBatch()`. Isso salva o lote como um arquivo JSON no diretório de persistência.
2.  **Marcação de Sucesso**: Se o `sink` confirma o recebimento do lote, o `dispatcher` chama `MarkBatchSuccess()`. Isso remove o arquivo correspondente do disco e da lista de lotes pendentes em memória.
3.  **Marcação de Falha**: Se o envio falhar, o `dispatcher` chama `MarkBatchFailed()`. O arquivo no disco é mantido, e as informações sobre a falha (motivo, número de tentativas) são atualizadas no arquivo.
4.  **Recuperação na Inicialização**: Ao iniciar, o `BatchPersistence` varre o diretório de persistência em busca de arquivos de lotes que não foram enviados com sucesso em execuções anteriores (`loadPersistedBatches`).
5.  **Loop de Recuperação**: Uma goroutine em background (`recoveryLoop`) verifica periodicamente os lotes pendentes. Quando um lote está pronto para uma nova tentativa (com base em uma política de `exponential backoff`), ele é disponibilizado para ser reprocessado.
6.  **Limpeza (Cleanup)**: Outra goroutine (`cleanupLoop`) remove arquivos de lotes muito antigos que excederam o `BatchTTL`, para evitar o acúmulo indefinido de dados que não podem ser recuperados.

## Papel e Importância

O módulo `persistence` é uma camada de **segurança e durabilidade** que complementa o `buffer` e a `DLQ`. Enquanto o `buffer` lida com a indisponibilidade temporária dos `sinks` e a `DLQ` armazena logs que falharam individualmente, a persistência de lotes protege contra a perda de dados em trânsito devido a uma falha na própria aplicação `log_capturer_go`.

Sua importância é:

- **Durabilidade**: Garante que, uma vez que um lote é formado, ele não será perdido mesmo que a aplicação seja reiniciada abruptamente antes que o envio seja concluído.
- **Recuperação de Falhas**: Fornece um mecanismo robusto para recuperar e reenviar lotes inteiros que falharam, mantendo a ordem e a integridade dos dados dentro do lote.
- **Confiabilidade de ponta a ponta**: Juntamente com o `PositionManager` e a `DLQ`, ele forma uma cadeia de componentes que trabalham juntos para garantir que cada log seja processado "pelo menos uma vez" (at-least-once delivery).

## Configurações Aplicáveis

As configurações para o `BatchPersistence` são definidas na seção `persistence` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita a persistência de lotes.
- **`directory`**: O diretório onde os arquivos de lote serão salvos.
- **`max_pending_batches`**: O número máximo de lotes a serem mantidos em memória. Se este limite for atingido, novos lotes podem ser descartados.
- **`batch_timeout`**: O tempo após o qual um lote pendente é considerado "velho" e pode ser limpo.
- **`max_recovery_retries`**: O número máximo de vezes que o sistema tentará recuperar um lote falho.
- **`recovery_backoff_base`** e **`recovery_backoff_max`**: Parâmetros para a estratégia de `exponential backoff` nas tentativas de recuperação.
- **`batch_ttl`**: O tempo de vida máximo para um lote persistido antes de ser removido pela rotina de limpeza.

## Problemas e Melhorias

### Problemas Potenciais:

- **I/O de Disco**: A escrita de cada lote no disco antes do envio pode se tornar um gargalo de performance, especialmente sob alta carga. É um trade-off entre performance e durabilidade.
- **Gerenciamento de Estado**: Manter o estado de centenas ou milhares de lotes pendentes (tanto em memória quanto em disco) pode ser complexo e consumir recursos.
- **Duplicação na Recuperação**: Se um lote for enviado com sucesso, mas a aplicação falhar antes de poder chamar `MarkBatchSuccess`, o mesmo lote pode ser reenviado após a reinicialização, causando duplicatas no `sink`. Os `sinks` de destino precisam ser idempotentes ou o sistema de deduplicação precisa estar ativo para lidar com isso.

### Sugestões de Melhorias:

- **Formato de Arquivo Otimizado**: Em vez de JSON, usar um formato binário mais eficiente (como Protocol Buffers) para serializar os lotes, reduzindo o tamanho dos arquivos e o tempo de I/O.
- **Agrupamento de Escritas**: Em vez de escrever um arquivo para cada lote, agrupar múltiplos lotes em um único arquivo de log (similar a um Write-Ahead Log), o que pode melhorar significativamente a performance de escrita.
- **Interface de Recuperação**: Expor os lotes pendentes através de uma API para que um administrador possa ver o que está pendente e, se necessário, acionar manualmente a recuperação ou o descarte de um lote específico.
- **Estratégias de Recuperação Configuráveis**: Permitir a configuração de diferentes estratégias de recuperação, como "tentar um `sink` alternativo" se o `sink` original do lote continuar falhando.
