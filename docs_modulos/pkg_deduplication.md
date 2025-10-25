# Módulo: pkg/deduplication

## Estrutura

*   `deduplication_manager.go`: Este arquivo contém o `DeduplicationManager`, que é responsável por detectar e filtrar entradas de log duplicadas.
*   `deduplication_manager_test.go`: Contém testes unitários para o `DeduplicationManager`.

## Como funciona

O módulo `pkg/deduplication` fornece um mecanismo para identificar e descartar entradas de log duplicadas para reduzir o ruído e economizar espaço de armazenamento.

1.  **Inicialização (`NewDeduplicationManager`):**
    *   Cria uma nova instância do `DeduplicationManager`.
    *   Define valores padrão para o tamanho do cache, TTL (Time To Live) e intervalo de limpeza.
    *   Inicializa um cache na memória para armazenar os hashes das entradas de log vistas recentemente. O cache usa uma política de remoção LRU (Least Recently Used).

2.  **Detecção de Duplicatas (`IsDuplicate`):**
    *   Quando uma nova entrada de log é recebida, a função `IsDuplicate` é chamada.
    *   Ela gera um hash do conteúdo da entrada de log (e, opcionalmente, seu timestamp e ID de origem).
    *   Em seguida, verifica se o hash existe no cache.
    *   **Se o hash existir:** A entrada de log é considerada uma duplicata e é descartada. A entrada do cache é movida para o início da lista LRU.
    *   **Se o hash não existir:** A entrada de log é considerada única. Seu hash é adicionado ao cache e a entrada de log pode prosseguir pelo pipeline de processamento.

3.  **Gerenciamento de Cache:**
    *   **Remoção LRU:** Quando o cache atinge seu tamanho máximo, a entrada menos recentemente usada é removida para dar espaço a novas entradas.
    *   **Expiração de TTL:** Uma goroutine em segundo plano (`cleanupLoop`) é executada periodicamente para remover do cache as entradas que excederam seu TTL.

## Papel e Importância

O módulo `pkg/deduplication` é importante para melhorar a eficiência e a qualidade dos dados de log. Seus principais papéis são:

*   **Redução de Ruído:** Filtra entradas de log duplicadas, que podem ser comuns em alguns sistemas (por exemplo, devido a novas tentativas ou registro mal configurado). Isso torna mais fácil para os operadores encontrarem informações importantes nos logs.
*   **Economia de Armazenamento:** Ao descartar logs duplicados, reduz a quantidade de dados que precisam ser armazenados no sistema de registro downstream, o que pode levar a uma economia de custos significativa.
*   **Melhora de Desempenho:** Pode melhorar o desempenho do sistema de registro downstream, reduzindo o número de entradas de log que precisam ser indexadas e armazenadas.

## Configurações

O módulo `deduplication` é configurado através da seção `dispatcher.deduplication_config` do arquivo `config.yaml`. As principais configurações incluem:

*   `max_cache_size`: O número máximo de hashes de log a serem armazenados no cache.
*   `ttl`: A quantidade de tempo para manter um hash de log no cache antes que ele expire.
*   `cleanup_interval`: Com que frequência executar o processo de limpeza para entradas expiradas.
*   `hash_algorithm`: O algoritmo de hash a ser usado (por exemplo, `sha256`).
*   `include_timestamp`: Se deve incluir o timestamp do log no hash.
*   `include_source_id`: Se deve incluir o ID de origem do log no hash.

## Problemas e Melhorias

*   **Colisões de Hash:** Embora improvável com uma boa função de hash como SHA256, existe uma possibilidade teórica de colisões de hash, o que poderia fazer com que uma entrada de log única fosse incorretamente identificada como uma duplicata.
*   **Uso de Memória:** O cache é armazenado na memória, portanto, um tamanho de cache muito grande pode consumir uma quantidade significativa de memória.
*   **Deduplicação Distribuída:** A implementação atual é local para uma única instância da aplicação `log_capturer_go`. Em uma implantação distribuída com várias instâncias, um cache distribuído (por exemplo, Redis) seria necessário para realizar a deduplicação em todas as instâncias.
