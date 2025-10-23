# Módulo Deduplication

## Estrutura e Operação

O módulo `deduplication` é responsável por identificar e filtrar logs duplicados, evitando que a mesma mensagem seja processada e enviada aos `sinks` múltiplas vezes. Isso é especialmente útil em cenários onde as fontes de log podem, por algum motivo, reenviar logs que já foram coletados.

### Principais Componentes da Estrutura:

- **`DeduplicationManager`**: A estrutura principal que gerencia um cache de hashes de logs já vistos.
- **`CacheEntry`**: Representa uma entrada no cache. Cada entrada armazena o hash de um log, quando ele foi visto pela primeira vez e a última vez, e um contador de "hits".
- **Cache LRU (Least Recently Used)**: O cache é implementado como uma lista duplamente ligada para seguir uma política de remoção LRU. Quando o cache atinge sua capacidade máxima, a entrada menos recentemente usada é removida para dar lugar a uma nova.
- **TTL (Time To Live)**: As entradas no cache também têm um tempo de vida (`TTL`). Após esse tempo, a entrada é considerada expirada e será removida pela rotina de limpeza.

### Fluxo de Operação:

1.  **Geração de Hash**: Para cada log que chega, o `DeduplicationManager` gera um hash único. Esse hash pode ser baseado apenas na mensagem do log, ou pode incluir também o `source_id` e um `timestamp` truncado para maior precisão.
2.  **Verificação no Cache**: O hash gerado é procurado no cache em memória.
3.  **Detecção de Duplicata**: Se o hash já existe no cache e a entrada não expirou (TTL), o log é considerado uma duplicata e é descartado.
4.  **Adição ao Cache**: Se o hash não existe, o log é considerado novo. Um novo `CacheEntry` é criado e adicionado ao cache. A entrada também é adicionada ao início da lista LRU.
5.  **Limpeza (Cleanup)**: Periodicamente, uma goroutine de `cleanup` varre o cache e remove as entradas que já expiraram (passaram do TTL). Se o cache estiver acima de um `threshold` de uso, ele também remove as entradas menos recentemente usadas (LRU) para liberar espaço.

## Papel e Importância

O módulo `deduplication` desempenha um papel importante na **otimização do armazenamento e na redução de ruído** nos sistemas de log.

Sua importância se traduz em:

- **Economia de Armazenamento**: Evita que logs repetidos consumam espaço desnecessário nos `sinks` (Loki, Elasticsearch, etc.), o que pode levar a uma redução de custos.
- **Análise Mais Limpa**: Reduz o "ruído" nos logs, facilitando a análise e a busca por eventos importantes, já que os analistas não precisam lidar com múltiplas cópias da mesma mensagem.
- **Melhora na Performance**: Ao descartar logs duplicados no início do pipeline, ele economiza recursos de CPU e rede que seriam gastos no processamento e envio desses logs.

## Configurações Aplicáveis

As configurações para o `DeduplicationManager` são definidas na seção `deduplication` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita a funcionalidade de deduplicação.
- **`max_cache_size`**: O número máximo de hashes de log a serem mantidos no cache em memória.
- **`ttl`**: O tempo de vida de uma entrada no cache (ex: "1h"). Após esse tempo, um log com o mesmo hash será considerado novo novamente.
- **`cleanup_interval`**: A frequência com que a rotina de limpeza do cache é executada.
- **`hash_algorithm`**: O algoritmo de hash a ser usado (atualmente, o padrão é `sha256`).
- **`include_timestamp`** e **`include_source_id`**: Controlam se o timestamp e o ID da fonte devem ser incluídos na geração do hash para uma detecção de duplicatas mais granular.

## Problemas e Melhorias

### Problemas Potenciais:

- **Consumo de Memória**: O cache de hashes é mantido em memória. Um `max_cache_size` muito grande pode consumir uma quantidade significativa de memória RAM.
- **Colisões de Hash**: Embora raro com `sha256`, colisões de hash são teoricamente possíveis e poderiam fazer com que um log legítimo fosse descartado como duplicata.
- **Escolha do TTL**: Um `TTL` muito curto pode reduzir a eficácia da deduplicação, enquanto um `TTL` muito longo pode aumentar o consumo de memória.

### Sugestões de Melhorias:

- **Cache Persistente**: Para sobreviver a reinicializações, o cache de deduplicação poderia ser persistido em disco (usando, por exemplo, um banco de dados chave-valor como o BoltDB).
- **Filtro de Bloom**: Para cenários que exigem um cache muito maior com menor consumo de memória, um Filtro de Bloom poderia ser usado como uma primeira camada de verificação probabilística, antes de consultar o cache exato.
- **Deduplicação Semântica**: Implementar uma forma de deduplicação "semântica" que possa identificar logs que são funcionalmente duplicados, mesmo que não sejam textualmente idênticos (ex: ignorando timestamps ou IDs de requisição dentro da mensagem).
- **Métricas de Eficácia**: Adicionar métricas para rastrear não apenas o número de duplicatas encontradas, mas também a taxa de acerto (hit rate) do cache e a taxa de evicção, para ajudar a ajustar os parâmetros de `max_cache_size` e `TTL`.
