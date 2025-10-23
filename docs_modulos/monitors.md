# Módulo Monitors

## Estrutura e Operação

O módulo `monitors` é responsável por coletar logs de diversas fontes. Ele é composto por sub-módulos especializados, cada um focado em um tipo de fonte de log, como arquivos locais e containers Docker.

### Principais Componentes da Estrutura:

- **`FileMonitor`**: Monitora arquivos de log no sistema de arquivos. Utiliza o `fsnotify` para detectar alterações em tempo real e um mecanismo de polling como fallback. Ele mantém o controle da posição de leitura em cada arquivo para evitar o reenvio de logs já processados.
- **`ContainerMonitor`**: Monitora os logs de containers Docker. Ele se conecta ao daemon do Docker, ouve eventos de ciclo de vida dos containers (start, stop, etc.) e faz o streaming dos logs (stdout/stderr) dos containers em execução.
- **`monitoredFile` e `monitoredContainer`**: Estruturas internas que mantêm o estado de cada arquivo ou container que está sendo monitorado, como o caminho, a posição de leitura, labels e o estado atual.

### Fluxo de Operação:

1.  **Inicialização**: Os monitores são inicializados pelo módulo `app` com base nas configurações fornecidas.
2.  **Descoberta**: Os monitores descobrem as fontes de log que devem ser monitoradas. O `FileMonitor` varre os diretórios configurados, enquanto o `ContainerMonitor` lista os containers em execução.
3.  **Monitoramento Ativo**: Cada monitor executa um loop em background:
    - O `FileMonitor` reage a eventos de escrita nos arquivos e lê as novas linhas.
    - O `ContainerMonitor` faz o streaming dos logs dos containers e reage a eventos do Docker para iniciar ou parar o monitoramento de novos containers.
4.  **Envio para o Dispatcher**: Os logs coletados são formatados em uma estrutura `LogEntry` e enviados para o módulo `dispatcher` para serem processados e enviados aos `sinks`.
5.  **Gerenciamento de Estado**: Os monitores interagem com o `PositionManager` para salvar e recuperar a posição de leitura, garantindo a continuidade da coleta de logs mesmo após reinicializações.

## Papel e Importância

O módulo `monitors` é a **porta de entrada dos dados** no `log_capturer_go`. Sua principal função é a coleta de logs de forma confiável e eficiente a partir de diferentes fontes. A importância deste módulo reside em sua capacidade de:

- **Abstrair as Fontes de Log**: Ele fornece uma interface unificada para a coleta de logs, independentemente de sua origem (arquivos, containers, etc.).
- **Garantir a Não Perda de Dados**: Através do gerenciamento de posição, ele garante que todos os logs sejam coletados, mesmo que a aplicação seja reiniciada.
- **Adaptabilidade**: A arquitetura modular permite que novos tipos de monitores (ex: para Kubernetes, journald) sejam adicionados no futuro com relativa facilidade.

## Configurações Aplicáveis

As configurações para os monitores são definidas nas seções `file_monitor_service` e `container_monitor` do `config.yaml`:

### FileMonitor:

- **`enabled`**: Habilita o monitoramento de arquivos.
- **`watch_directories`**: Lista de diretórios a serem monitorados.
- **`include_patterns` / `exclude_patterns`**: Padrões de nome de arquivo para incluir ou excluir.
- **`poll_interval`**: Intervalo de tempo para verificar arquivos em busca de alterações.

### ContainerMonitor:

- **`enabled`**: Habilita o monitoramento de containers Docker.
- **`socket_path`**: Caminho para o socket do Docker.
- **`include_labels` / `exclude_labels`**: Filtra containers com base em seus labels.
- **`include_names` / `exclude_names`**: Filtra containers com base em seus nomes.

## Problemas e Melhorias

### Problemas Potenciais:

- **Rotação de Logs**: Lidar com a rotação de arquivos de log pode ser complexo. O monitor precisa detectar que um arquivo foi rotacionado para não perder logs ou ler o mesmo log duas vezes.
- **Performance do `fsnotify`**: Em sistemas com um número muito grande de arquivos, o `fsnotify` pode consumir muitos recursos. O polling, por outro lado, pode introduzir latência.
- **Conexão com o Docker**: A conexão com o daemon do Docker pode ser instável, exigindo uma lógica robusta de reconexão.

### Sugestões de Melhorias:

- **Detecção de Rotação por Inode**: Melhorar a detecção de rotação de arquivos utilizando o `inode` em vez do nome do arquivo, o que é mais robusto.
- **Monitor de Journald**: Adicionar um novo tipo de monitor para coletar logs do `systemd-journald` em sistemas Linux.
- **Monitor de Kubernetes**: Implementar um monitor que utilize a API do Kubernetes para descobrir e coletar logs de pods, enriquecendo os logs com metadados do Kubernetes.
- **Backpressure no Nível do Monitor**: Implementar um mecanismo de backpressure nos próprios monitores para que eles reduzam a velocidade de leitura se o `dispatcher` estiver sobrecarregado.
