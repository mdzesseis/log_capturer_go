# Módulo: pkg/buffer

## Estrutura

*   `disk_buffer.go`: Este arquivo contém o `DiskBuffer`, que implementa um buffer persistente baseado em disco para entradas de log.

## Como funciona

O módulo `pkg/buffer` fornece um mecanismo para armazenar em buffer as entradas de log no disco, o que é crucial para garantir a durabilidade dos dados e lidar com a contrapressão (backpressure) dos coletores (sinks) downstream.

1.  **Inicialização (`NewDiskBuffer`):**
    *   Cria uma nova instância de `DiskBuffer`.
    *   Define valores padrão para o diretório do buffer, tamanhos de arquivo e políticas de retenção.
    *   Cria o diretório base para os arquivos de buffer, se ele não existir.
    *   Verifica a existência de arquivos de buffer de execuções anteriores para serem recuperados.
    *   Inicia goroutines em segundo plano para sincronizar periodicamente os dados com o disco (`syncLoop`) e limpar arquivos antigos (`cleanupLoop`).

2.  **Escrevendo no Buffer (`Write`):**
    *   Quando uma entrada de log é escrita no buffer, ela é primeiro encapsulada em uma struct `BufferEntry`, que inclui um timestamp e um checksum.
    *   A `BufferEntry` é então serializada para JSON.
    *   Os dados serializados, prefixados com seu comprimento, são escritos no arquivo de buffer atual.
    *   Se a compressão estiver habilitada, os dados são comprimidos usando gzip antes de serem escritos no disco.
    *   Se o arquivo de buffer atual atingir seu tamanho máximo, um novo arquivo é criado (`rotateFile`).

3.  **Lendo do Buffer (`ReadAll`):**
    *   A função `ReadAll` é usada para recuperar entradas de log do buffer, por exemplo, após uma reinicialização.
    *   Ela lê todos os arquivos de buffer, desserializa as `BufferEntry`s e verifica seus checksums.
    *   Em seguida, retorna uma fatia das `LogEntry`s recuperadas.

4.  **Gerenciamento de Arquivos (`rotateFile`, `cleanupLoop`):**
    *   A função `rotateFile` é chamada quando o arquivo de buffer atual atinge seu tamanho máximo. Ela fecha o arquivo atual e cria um novo.
    *   O `cleanupLoop` executa periodicamente `performCleanup`, que remove arquivos de buffer antigos com base em sua idade, no tamanho total do buffer e no número máximo de arquivos.

## Papel e Importância

O módulo `pkg/buffer` desempenha um papel crítico na confiabilidade e resiliência da aplicação `log_capturer_go`.

*   **Durabilidade dos Dados:** Ao escrever as entradas de log no disco, ele garante que elas não sejam perdidas se a aplicação falhar ou for reiniciada.
*   **Manuseio de Contrapressão:** Atua como um buffer entre o `Dispatcher` e os coletores. Se um coletor estiver indisponível ou lento, o `Dispatcher` pode escrever as entradas de log no `DiskBuffer` em vez de descartá-las ou bloquear.
*   **Desacoplamento:** Desacopla a captura e o processamento de logs da entrega de logs, permitindo que a aplicação continue a capturar logs mesmo que os coletores downstream estejam indisponíveis.

## Configurações

A seção `disk_buffer` do arquivo `config.yaml` é usada para configurar este módulo. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita o buffer de disco.
*   `directory`: O diretório onde os arquivos de buffer são armazenados.
*   `max_file_size`: O tamanho máximo de um único arquivo de buffer.
*   `max_total_size`: O tamanho total máximo de todos os arquivos de buffer.
*   `compression_enabled`: Habilita ou desabilita a compressão dos arquivos de buffer.
*   `retention_period`: Por quanto tempo manter os arquivos de buffer antes de excluí-los.

## Problemas e Melhorias

*   **Desempenho de Recuperação:** A leitura de todos os arquivos de buffer na inicialização pode ser lenta se houver uma grande quantidade de dados no buffer. O processo de recuperação poderia ser otimizado, por exemplo, usando um arquivo de índice para rastrear as entradas em cada arquivo de buffer.
*   **Verificação de Checksum:** A verificação de checksum é um bom recurso, mas poderia ser mais robusta. Por exemplo, poderia usar uma função de hash mais resistente a colisões.
*   **Criptografia:** A configuração inclui uma opção `encryption_enabled`, mas a implementação está ausente. Este seria um recurso valioso para proteger dados de log sensíveis em repouso.
*   **Recuperação Seletiva:** A função `ReadAll` atual recupera todas as entradas do buffer. Uma implementação mais avançada poderia permitir a recuperação seletiva de entradas, por exemplo, com base em seu timestamp ou origem.
