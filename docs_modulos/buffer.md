# Módulo Buffer

## Estrutura e Operação

O módulo `buffer` fornece um buffer persistente em disco para os logs, atuando como uma camada de segurança para prevenir a perda de dados em caso de falha ou indisponibilidade dos `sinks` (destinos).

### Principais Componentes da Estrutura:

- **`DiskBuffer`**: A estrutura principal que gerencia a escrita e a leitura de logs em arquivos no disco.
- **`DiskBufferConfig`**: Contém as configurações do buffer, como o diretório de armazenamento, o tamanho máximo dos arquivos, as políticas de retenção e a compressão.
- **`BufferEntry`**: Representa uma entrada no buffer, contendo o log original e um `checksum` para garantir a integridade dos dados.

### Fluxo de Operação:

1.  **Inicialização**: O `DiskBuffer` é inicializado, criando o diretório de buffer se ele não existir e procurando por arquivos de buffer antigos que possam precisar de recuperação.
2.  **Escrita**: Quando um log precisa ser "bufferizado" (geralmente porque um `sink` está indisponível), ele é escrito no arquivo de buffer atual. Cada entrada é prefixada com seu tamanho e um checksum para garantir a integridade.
3.  **Rotação de Arquivos**: Quando o arquivo de buffer atual atinge o tamanho máximo configurado (`max_file_size`), ele é "rotacionado" (renomeado) e um novo arquivo é criado para as próximas escritas.
4.  **Recuperação**: Na inicialização da aplicação, o `DiskBuffer` verifica a existência de arquivos de buffer de execuções anteriores. Se encontrados, esses logs podem ser lidos e reenviados, garantindo que nenhum dado seja perdido entre reinicializações.
5.  **Limpeza (Cleanup)**: Periodicamente, uma rotina de limpeza é executada para remover arquivos de buffer antigos que já expiraram (com base no `retention_period`) ou que excedem os limites de número de arquivos (`max_files`) ou de tamanho total do disco (`max_total_size`).

## Papel e Importância

O módulo `buffer` é **crítico para a confiabilidade e a resiliência** do `log_capturer_go`. Ele funciona como uma rede de segurança que protege contra a perda de dados.

Sua importância se manifesta em:

- **Durabilidade**: Ao persistir os logs em disco, o `buffer` garante que os dados não sejam perdidos se a aplicação for reiniciada ou se os `sinks` de destino ficarem indisponíveis por um longo período.
- **Desacoplamento**: Ele desacopla completamente os produtores de logs (monitores) dos consumidores (sinks). Os monitores podem continuar coletando logs mesmo que os sinks estejam offline.
- **Gerenciamento de Carga**: Em cenários de picos de logs, o buffer em disco pode absorver o excesso de dados que não pode ser processado em tempo real, evitando o descarte de logs.

## Configurações Aplicáveis

As configurações para o `DiskBuffer` são definidas na seção `disk_buffer` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita o buffer em disco.
- **`directory`**: O diretório onde os arquivos de buffer serão armazenados.
- **`max_file_size`**: O tamanho máximo (em bytes) que um arquivo de buffer pode atingir antes de ser rotacionado.
- **`max_total_size`**: O tamanho máximo total (em bytes) que o diretório de buffer pode ocupar.
- **`compression_enabled`**: Habilita a compressão (gzip) dos arquivos de buffer para economizar espaço em disco.
- **`retention_period`**: O tempo que um arquivo de buffer será mantido antes de ser elegível para a limpeza (ex: "24h").

## Problemas e Melhorias

### Problemas Potenciais:

- **I/O de Disco**: A escrita constante no disco pode se tornar um gargalo de performance, especialmente em discos lentos.
- **Corrupção de Arquivos**: Falhas durante a escrita podem levar a arquivos de buffer corrompidos. O uso de checksums ajuda a detectar isso, mas não previne a perda do dado corrompido.
- **Gerenciamento de Espaço**: Se a limpeza não for configurada corretamente ou se os `sinks` ficarem indisponíveis por muito tempo, o buffer pode consumir todo o espaço em disco disponível.

### Sugestões de Melhorias:

- **Write-Ahead Log (WAL)**: Implementar um verdadeiro formato de Write-Ahead Log para maior robustez contra corrupção de dados.
- **Compressão Adaptativa**: Adaptar o nível de compressão com base na carga da CPU e na taxa de escrita, para balancear o uso de CPU e o espaço em disco.
- **Múltiplos Diretórios de Buffer**: Permitir a configuração de múltiplos diretórios de buffer em diferentes discos para distribuir a carga de I/O.
- **Recuperação Inteligente**: Melhorar a lógica de recuperação para que ela possa reenviar os logs de forma mais inteligente, talvez para um `sink` diferente se o original ainda estiver indisponível.
