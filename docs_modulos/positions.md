# Módulo Positions

## Estrutura e Operação

O módulo `positions` é responsável por rastrear a posição de leitura em cada fonte de log (arquivos e containers). Sua função é garantir que, em caso de reinicialização, a aplicação saiba exatamente de onde continuar a coleta de logs, evitando a perda de dados ou o envio de duplicatas.

### Principais Componentes da Estrutura:

- **`PositionBufferManager`**: Atua como um orquestrador de alto nível. Ele gerencia os `managers` de posição específicos para arquivos e containers, e implementa uma lógica de buffer em memória para otimizar a escrita das posições em disco.
- **`FilePositionManager`**: Especializado em gerenciar as posições de leitura de arquivos. Ele armazena o `offset` (em bytes) do último log lido. Para lidar com a rotação de arquivos, ele também armazena o `inode` e o `device` do arquivo, o que permite detectar se um arquivo foi substituído por outro com o mesmo nome.
- **`ContainerPositionManager`**: Especializado em gerenciar as posições de leitura de containers Docker. Como os logs de containers são streams, ele armazena o `timestamp` do último log lido (`since`), garantindo que, ao se reconectar, ele só peça os logs a partir daquele ponto no tempo.

### Fluxo de Operação:

1.  **Carregamento**: Na inicialização, os `managers` de posição carregam os arquivos de estado (ex: `file_positions.json`, `container_positions.json`) do disco para a memória.
2.  **Atualização**: Conforme os `monitors` leem novos logs, eles notificam o `PositionBufferManager` sobre a nova posição (o `offset` para arquivos ou o `timestamp` para containers).
3.  **Buffering**: As atualizações de posição são mantidas em memória (são "dirty") para evitar escritas constantes no disco, o que seria ineficiente.
4.  **Flush Periódico**: Em intervalos regulares (`flush_interval`), o `PositionBufferManager` verifica se há posições "dirty" e, se houver, chama os métodos `SavePositions()` dos `managers` específicos para persistir as posições atualizadas em seus respectivos arquivos JSON no disco.
5.  **Limpeza**: Periodicamente, uma rotina de limpeza (`cleanupLoop`) remove as informações de posição de arquivos ou containers que não existem mais ou que não são monitorados há muito tempo, para evitar que os arquivos de estado cresçam indefinidamente.

## Papel e Importância

O módulo `positions` é **absolutamente crítico para a confiabilidade e a garantia de entrega "at-least-once"** (pelo menos uma vez) do `log_capturer_go`.

Sua importância reside em:

- **Prevenção de Perda de Dados**: Garante que, se a aplicação for reiniciada, ela não perca nenhum log que foi escrito enquanto ela estava offline.
- **Prevenção de Duplicatas**: Impede que a aplicação releia e reenvie logs que já foram processados antes de uma reinicialização.
- **Resiliência**: Torna o processo de coleta de logs resiliente a falhas e reinícios, uma característica essencial para um sistema de nível de produção.
- **Gerenciamento de Rotação de Arquivos**: A capacidade de detectar a rotação de arquivos (usando `inode`) é uma funcionalidade sofisticada e crucial para monitorar arquivos de log gerenciados por ferramentas como `logrotate`.

## Configurações Aplicáveis

As configurações para o gerenciamento de posições são definidas na seção `positions` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita o rastreamento de posições.
- **`directory`**: O diretório onde os arquivos de estado (`..._positions.json`) serão armazenados.
- **`flush_interval`**: A frequência com que as posições em memória são salvas no disco.
- **`max_memory_buffer`**: O número máximo de atualizações de posição a serem mantidas em memória antes de forçar um flush.
- **`force_flush_on_exit`**: Se `true`, garante que todas as posições pendentes sejam salvas no disco durante o desligamento da aplicação.
- **`cleanup_interval`** e **`max_position_age`**: Configurações para a limpeza de entradas de posição antigas e não utilizadas.

## Problemas e Melhorias

### Problemas Potenciais:

- **Corrupção do Arquivo de Posição**: Se o arquivo de posição for corrompido, a aplicação pode perder o estado de leitura e reler todos os logs do início, causando uma grande quantidade de duplicatas.
- **Performance de I/O**: Embora o `BufferManager` otimize a escrita, em um sistema com milhares de fontes de log, o processo de `flush` ainda pode causar picos de I/O no disco.
- **Sincronização de Clock**: O rastreamento de posição para containers é baseado em `timestamp`. Se o clock do sistema onde o `log_capturer_go` está rodando e o clock do Docker daemon não estiverem sincronizados, pode haver perda ou duplicação de logs.

### Sugestões de Melhorias:

- **Backup e Rollback do Arquivo de Posição**: Implementar um sistema de backup para os arquivos de posição, permitindo a restauração para um estado anterior em caso de corrupção.
- **Formato de Armazenamento Otimizado**: Em vez de JSON, usar um formato de banco de dados embarcado mais robusto e eficiente, como o BoltDB ou o Badger, para armazenar as posições. Isso melhoraria a performance e a resiliência contra corrupção.
- **Estratégia de Flush Adaptativa**: Tornar o `flush_interval` adaptativo. Em períodos de alta atividade, fazer o flush com mais frequência. Em períodos de baixa atividade, fazer com menos frequência para economizar recursos.
- **Validação de Posição na Inicialização**: Ao carregar as posições, validar se os arquivos/containers ainda existem e se os metadados (como `inode`) ainda correspondem, para evitar começar a ler da posição errada.
