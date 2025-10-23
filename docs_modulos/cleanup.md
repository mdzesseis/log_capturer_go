# Módulo Cleanup

## Estrutura e Operação

O módulo `cleanup` (implementado como `DiskSpaceManager`) é responsável por gerenciar o espaço em disco utilizado pela aplicação, realizando limpezas automáticas para evitar que o disco fique cheio.

### Principais Componentes da Estrutura:

- **`DiskSpaceManager`**: A estrutura principal que executa o monitoramento e a limpeza dos diretórios configurados.
- **`Config`**: Contém a configuração do gerenciador, incluindo a lista de diretórios a serem monitorados e os `thresholds` (limiares) de uso de disco.
- **`DirectoryConfig`**: Define as regras de limpeza para um diretório específico, como o tamanho máximo, a idade de retenção e o número máximo de arquivos.

### Fluxo de Operação:

1.  **Monitoramento Periódico**: O `DiskSpaceManager` executa um loop em background que, em intervalos regulares (`check_interval`), verifica o estado dos diretórios configurados.
2.  **Verificação de Políticas**: Para cada diretório, ele aplica uma série de políticas de limpeza em uma ordem específica:
    - **Limpeza por Idade (`cleanupByAge`)**: Remove arquivos mais antigos que a idade de retenção configurada (`retention_days`).
    - **Limpeza por Tamanho (`cleanupBySize`)**: Se o tamanho total do diretório exceder o `max_size_mb`, ele remove os arquivos mais antigos até que o tamanho total fique abaixo do limite.
    - **Limpeza por Contagem (`cleanupByCount`)**: Se o número de arquivos no diretório exceder o `max_files`, ele remove os arquivos mais antigos até que o número de arquivos fique dentro do limite.
3.  **Verificação Geral do Disco**: Além de gerenciar os diretórios específicos, o módulo também pode verificar o espaço livre geral do disco e emitir alertas se ele estiver abaixo dos `thresholds` de `warning` ou `critical`.

## Papel e Importância

O módulo `cleanup` é **essencial para a operação de longo prazo e a estabilidade** do `log_capturer_go`. Sem um gerenciamento de disco adequado, os logs e outros arquivos gerados pela aplicação (como os de buffer e de posição) poderiam consumir todo o espaço em disco, causando a falha não apenas do `log_capturer_go`, mas também de outros serviços no mesmo sistema.

Sua importância reside em:

- **Prevenção de Falhas**: Evita que a aplicação pare de funcionar devido à falta de espaço em disco.
- **Automação**: Automatiza a tarefa de limpeza de arquivos antigos, que de outra forma teria que ser feita manualmente ou por scripts externos.
- **Controle de Custos**: Em ambientes de nuvem, o controle do uso de disco pode ter um impacto direto nos custos de armazenamento.

## Configurações Aplicáveis

As configurações para o `DiskSpaceManager` são definidas na seção `disk_cleanup` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita o gerenciamento de disco.
- **`check_interval`**: A frequência com que a verificação de limpeza será executada.
- **`directories`**: Uma lista de diretórios a serem gerenciados, cada um com suas próprias regras:
    - **`path`**: O caminho do diretório.
    - **`max_size_mb`**: O tamanho máximo em megabytes que o diretório pode atingir.
    - **`retention_days`**: O número de dias que os arquivos serão mantidos antes de serem elegíveis para limpeza.
    - **`max_files`**: O número máximo de arquivos a serem mantidos no diretório.
    - **`file_patterns`**: Padrões de nome de arquivo a serem considerados para a limpeza (ex: `*.log`).

## Problemas e Melhorias

### Problemas Potenciais:

- **Performance de `filepath.Walk`**: Em diretórios com um número muito grande de arquivos, a varredura (`filepath.Walk`) para encontrar os arquivos a serem limpos pode ser lenta e consumir recursos.
- **Regras de Limpeza Conflitantes**: Se as regras de limpeza (idade, tamanho, contagem) não forem bem configuradas, o sistema pode se comportar de maneira inesperada, removendo mais arquivos do que o desejado.
- **Permissões de Arquivo**: O `DiskSpaceManager` precisa ter as permissões corretas para listar e remover arquivos nos diretórios que está gerenciando.

### Sugestões de Melhorias:

- **Estratégias de Limpeza Configuráveis**: Permitir que o usuário escolha a estratégia de limpeza (ex: "remover os mais antigos primeiro" vs. "remover os maiores primeiro").
- **Limpeza "Dry Run"**: Adicionar um modo "dry run" que simula a limpeza e relata quais arquivos seriam removidos, sem de fato removê-los. Isso ajudaria os usuários a validar suas configurações.
- **Quarentena**: Em vez de remover os arquivos imediatamente, movê-los para um diretório de "quarentena" por um período configurável antes da exclusão definitiva.
- **Notificações**: Integrar com um sistema de notificações para alertar os administradores antes que uma limpeza automática seja executada, especialmente se for uma limpeza crítica devido ao baixo espaço em disco.
