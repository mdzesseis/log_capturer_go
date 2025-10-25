# Módulo: pkg/positions

## Estrutura

*   `buffer_manager.go`: Este arquivo contém o `PositionBufferManager`, que atua como uma fachada para o `ContainerPositionManager` e o `FilePositionManager`.
*   `container_positions.go`: Este arquivo contém o `ContainerPositionManager`, que é responsável por gerenciar as posições dos logs de contêineres.
*   `file_positions.go`: Este arquivo contém o `FilePositionManager`, que é responsável por gerenciar as posições dos logs de arquivos.
*   `position_manager_test.go`: Contém testes unitários para os gerenciadores de posição.

## Como funciona

O módulo `pkg/positions` é responsável por rastrear a última posição lida para cada fonte de log (contêiner ou arquivo). Isso é crucial para garantir que a aplicação possa continuar de onde parou após uma reinicialização, sem perder ou duplicar logs.

1.  **Gerenciadores de Posição (`ContainerPositionManager`, `FilePositionManager`):**
    *   Existem dois gerenciadores de posição separados: um para contêineres e um para arquivos.
    *   Cada gerenciador mantém um mapa na memória de posições, onde a chave é a fonte de log (ID do contêiner ou caminho do arquivo) e o valor é uma struct contendo as informações de posição (por exemplo, deslocamento, timestamp).
    *   O método `UpdatePosition` é chamado pelos monitores para atualizar a posição de uma fonte de log.
    *   O método `GetPosition` é usado para recuperar a última posição conhecida de uma fonte de log.

2.  **Gerenciador de Buffer (`PositionBufferManager`):**
    *   O `PositionBufferManager` atua como uma fachada para os dois gerenciadores de posição.
    *   Ele fornece um único ponto de entrada para atualizar e recuperar posições e delega as chamadas para o gerenciador apropriado com base no tipo de fonte de log.
    *   Ele também gerencia a descarga periódica de posições para o disco.

3.  **Persistência:**
    *   Os gerenciadores de posição salvam periodicamente seus mapas na memória em um arquivo JSON no disco (`container_positions.json` e `file_positions.json`).
    *   Quando a aplicação é iniciada, ela carrega as posições desses arquivos para a memória.

4.  **Limpeza:**
    *   Uma goroutine em segundo plano (`cleanupLoop`) remove periodicamente entradas de posição antigas e inativas do mapa na memória e dos arquivos no disco.

## Papel e Importância

O módulo `pkg/positions` é um componente crítico para a confiabilidade e integridade dos dados da aplicação `log_capturer_go`. Seus principais papéis são:

*   **Integridade dos Dados:** Evita a perda ou duplicação de entradas de log, garantindo que a aplicação sempre saiba de onde continuar a leitura após uma reinicialização.
*   **Resiliência:** Torna a aplicação mais resiliente a falhas e reinicializações.
*   **Eficiência:** Ao rastrear a última posição lida, evita a releitura e o reprocessamento de logs que já foram processados.

## Configurações

A seção `positions` do arquivo `config.yaml` é usada para configurar este módulo. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita o rastreamento de posição.
*   `directory`: O diretório onde os arquivos de posição são armazenados.
*   `flush_interval`: Com que frequência salvar as posições no disco.
*   `max_memory_buffer`: O número máximo de entradas de posição a serem mantidas na memória.
*   `force_flush_on_exit`: Se deve forçar uma descarga das posições para o disco quando a aplicação for encerrada.

## Problemas e Melhorias

*   **Formato de Armazenamento:** As posições são atualmente armazenadas em arquivos JSON. Um formato de armazenamento mais eficiente e compacto, como um formato binário ou um pequeno banco de dados embutido, poderia ser usado para melhorar o desempenho, especialmente para um grande número de fontes de log.
*   **Detecção de Rotação de Arquivos:** O `FilePositionManager` possui alguma lógica para detectar a rotação de arquivos com base no inode e no ID do dispositivo. Isso poderia ser tornado mais robusto para lidar com diferentes estratégias de rotação de logs.
*   **Concorrência:** O uso de um único mutex para proteger todo o mapa de posições pode se tornar um gargalo em um ambiente de alto throughput. Uma estratégia de bloqueio mais granular poderia ser usada para melhorar a concorrência.
