# Módulo: internal/monitors

## Estrutura

*   `container_monitor.go`: Contém a lógica para monitorar os logs de contêineres Docker.
*   `file_monitor.go`: Contém a lógica para monitorar arquivos de log no sistema de arquivos.

## Como funciona

O módulo `internal/monitors` é responsável por observar ativamente e capturar novas entradas de log de diferentes fontes.

#### `container_monitor.go`

1.  **Inicialização (`NewContainerMonitor`):**
    *   Cria um novo `ContainerMonitor`.
    *   Inicializa um pool de conexões Docker (`docker.PoolManager`) para gerenciar as conexões com o daemon do Docker.
    *   Configura um `TimestampValidator` para validar os timestamps dos logs capturados.
    *   Inicializa um `FeedbackGuard` para evitar que a aplicação capture seus próprios logs.

2.  **Iniciando o Monitor (`Start`):**
    *   Realiza uma varredura inicial dos contêineres em execução (`scanContainers`).
    *   Inicia um loop para escutar eventos do Docker (`eventsLoop`), como o início, parada ou morte de contêineres. Isso permite um monitoramento reativo.
    *   Inicia um loop de verificação de saúde (`healthCheckLoop`) para verificar periodicamente a saúde dos contêineres monitorados.

3.  **Captura de Log (`monitorContainer` e `readContainerLogs`):**
    *   Para cada contêiner sendo monitorado, ele abre um stream de logs usando a API do Docker.
    *   Ele lê o stream de logs, analisa as linhas de log e remove o cabeçalho de log do Docker.
    *   Em seguida, ele passa as entradas de log para o `Dispatcher` para processamento.
    *   Ele usa o `PositionManager` para rastrear a última entrada de log lida, para que possa continuar de onde parou após uma reinicialização.

#### `file_monitor.go`

1.  **Inicialização (`NewFileMonitor`):**
    *   Cria um novo `FileMonitor`.
    *   Inicializa um `fsnotify.Watcher` para observar eventos do sistema de arquivos.
    *   Configura um `TimestampValidator` e um `FeedbackGuard`.

2.  **Iniciando o Monitor (`Start`):**
    *   Realiza uma descoberta inicial de arquivos a serem monitorados com base na configuração (`discoverFiles`).
    *   Inicia um loop de monitoramento (`monitorLoop`) que escuta por eventos do sistema de arquivos do `fsnotify.Watcher`.

3.  **Captura de Log (`readFile`):**
    *   Quando um arquivo é escrito, a função `readFile` é chamada.
    *   Ela abre o arquivo e vai para a última posição conhecida (recuperada do `PositionManager`).
    *   Ela lê novas linhas do arquivo e as envia para o `Dispatcher`.
    *   Ela atualiza a posição no `PositionManager` após ler com sucesso as novas linhas.

## Papel e Importância

O módulo `monitors` é a principal fonte de dados para a aplicação `log_capturer_go`. Sua importância reside em:

*   **Ingestão de Dados:** É responsável por capturar todos os dados de log que a aplicação irá processar.
*   **Monitoramento em Tempo Real:** Fornece monitoramento em tempo real das fontes de log, permitindo o processamento e análise oportunos dos logs.
*   **Resiliência:** Ao usar um `PositionManager`, ele pode retomar a captura de logs após uma reinicialização sem perder ou duplicar logs.
*   **Flexibilidade:** Suporta o monitoramento de contêineres Docker e arquivos de log, tornando-o adequado para uma variedade de ambientes.

## Configurações

O módulo `monitors` é configurado através das seções `file_monitor_service` e `container_monitor` do arquivo `config.yaml`.

#### `file_monitor_service`

*   `enabled`: Habilita ou desabilita o monitor de arquivos.
*   `pipeline_file`: Caminho para o arquivo de configuração do pipeline de arquivos.
*   `poll_interval`: Com que frequência verificar alterações nos arquivos.
*   `read_buffer_size`: O tamanho do buffer para leitura de arquivos.
*   `recursive`: Se deve monitorar diretórios recursivamente.
*   `follow_symlinks`: Se deve seguir links simbólicos.

#### `container_monitor`

*   `enabled`: Habilita ou desabilita o monitor de contêineres.
*   `socket_path`: O caminho para o socket do Docker.
*   `include_labels`, `exclude_labels`: Filtros para incluir ou excluir contêineres com base em seus rótulos.
*   `include_names`, `exclude_names`: Filtros para incluir ou excluir contêineres com base em seus nomes.

## Problemas e Melhorias

*   **Fallback de Polling:** O `file_monitor.go` depende do `fsnotify`, que pode não funcionar em todos os sistemas de arquivos (ex: compartilhamentos de rede). Um fallback baseado em polling poderia ser implementado para melhor compatibilidade.
*   **Análise de Log de Contêiner:** O `container_monitor.go` faz uma análise básica do stream de log do Docker, mas poderia ser mais robusto para lidar com diferentes formatos de log e casos extremos.
*   **Gerenciamento de Recursos:** Os monitores abrem manipuladores de arquivos e conexões de rede. Embora pareçam ser gerenciados corretamente, um tratamento de erros e limpeza de recursos mais robustos poderiam ser adicionados para evitar vazamentos.
*   **Configuração Dinâmica:** Os monitores poderiam ser aprimorados para suportar atualizações de configuração dinâmicas, permitindo adicionar ou remover arquivos e contêineres monitorados sem reiniciar a aplicação.
