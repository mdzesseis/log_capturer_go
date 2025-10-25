# Módulo: pkg/persistence

## Estrutura

*   `batch_persistence.go`: Este arquivo contém o componente `BatchPersistence`, que é responsável por persistir lotes de log em disco para fins de recuperação.

## Como funciona

O módulo `pkg/persistence` fornece um mecanismo para salvar lotes de log em disco antes de serem enviados para um coletor. Isso permite que a aplicação recupere os lotes em caso de falha ou reinicialização.

1.  **Inicialização (`NewBatchPersistence`):**
    *   Cria uma nova instância de `BatchPersistence`.
    *   Define valores padrão para o diretório de persistência, tempo limite do lote e outros parâmetros de configuração.
    *   Carrega quaisquer lotes persistidos anteriormente do disco.

2.  **Persistindo Lotes (`PersistBatch`):**
    *   Quando um novo lote de logs é criado, a função `PersistBatch` é chamada.
    *   Ela cria uma struct `PersistedBatch`, que contém o ID do lote, as entradas de log, o tipo de coletor e outros metadados.
    *   O `PersistedBatch` é então serializado para JSON e gravado em um arquivo no disco.
    *   O lote também é adicionado a um mapa na memória de lotes pendentes.

3.  **Marcando Lotes como Bem-sucedidos ou com Falha (`MarkBatchSuccess`, `MarkBatchFailed`):**
    *   Quando um lote é enviado com sucesso para um coletor, a função `MarkBatchSuccess` é chamada. Isso remove o lote do mapa na memória e exclui o arquivo correspondente do disco.
    *   Se o envio de um lote falhar, a função `MarkBatchFailed` é chamada. Isso atualiza a contagem de novas tentativas e o motivo da falha no `PersistedBatch` e salva as informações atualizadas no disco.

4.  **Recuperação (`recoveryLoop`):**
    *   Uma goroutine em segundo plano (`recoveryLoop`) verifica periodicamente se há lotes pendentes que estão prontos para serem tentados novamente.
    *   Ela usa uma estratégia de backoff exponencial para determinar quando tentar novamente um lote com falha.
    *   Quando um lote está pronto para ser tentado novamente, ele é passado para uma função de retorno de chamada de recuperação (ainda não totalmente implementada).

5.  **Limpeza (`cleanupLoop`):**
    *   Outra goroutine em segundo plano (`cleanupLoop`) remove periodicamente arquivos de lote antigos e expirados do disco.

## Papel e Importância

O módulo `pkg/persistence` é um componente crítico para garantir a durabilidade e a confiabilidade do pipeline de processamento de logs. Seus principais papéis são:

*   **Durabilidade dos Dados:** Ao persistir os lotes no disco, ele garante que os dados de log não sejam perdidos em caso de falha ou reinicialização da aplicação.
*   **Recuperação:** Fornece um mecanismo para recuperar e tentar novamente automaticamente os lotes com falha, o que ajuda a garantir que todos os logs sejam eventualmente entregues ao seu destino.
*   **Resiliência:** Torna a aplicação mais resiliente a falhas transitórias, como interrupções de rede ou indisponibilidade temporária de um coletor.

## Configurações

O módulo `persistence` é configurado através da seção `batch_persistence` do arquivo `config.yaml`. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita a persistência de lotes.
*   `directory`: O diretório onde os arquivos de lote persistidos são armazenados.
*   `batch_timeout`: A quantidade de tempo a esperar antes de considerar um lote como falho.
*   `max_recovery_retries`: O número máximo de vezes para tentar recuperar um lote com falha.
*   `batch_ttl`: A quantidade de tempo para manter um arquivo de lote persistido antes que ele seja excluído.

## Problemas e Melhorias

*   **Retorno de Chamada de Recuperação:** O mecanismo de recuperação não está totalmente implementado. Uma função de retorno de chamada precisa ser fornecida para lidar com o reprocessamento de lotes recuperados.
*   **Formato de Armazenamento:** Os lotes são atualmente armazenados como arquivos JSON. Um formato de armazenamento mais eficiente e compacto, como Protocol Buffers, poderia ser usado para reduzir a pegada de armazenamento e melhorar o desempenho.
*   **Recuperação Seletiva:** O mecanismo de recuperação atual tenta novamente todos os lotes pendentes. Uma implementação mais avançada poderia permitir a recuperação seletiva de lotes, por exemplo, com base em seu tipo de coletor ou motivo da falha.
