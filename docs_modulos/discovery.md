# Módulo Service Discovery

## Estrutura e Operação

O módulo `discovery` é responsável pela descoberta automática de serviços que podem ser monitorados pelo `log_capturer_go`. Ele suporta diferentes métodos de descoberta, como Docker e arquivos, e é projetado para ser extensível a outras plataformas como Kubernetes.

### Principais Componentes da Estrutura:

- **`ServiceDiscovery` Struct**: A estrutura principal que gerencia o processo de descoberta. Ela contém as configurações, o cliente Docker, a lista de serviços descobertos e os callbacks para notificar sobre mudanças.
- **`Config` Struct**: Contém todas as configurações para o service discovery, incluindo os métodos de descoberta habilitados (Docker, file) e suas configurações específicas.
- **`DiscoveredService` Struct**: Representa um serviço descoberto, contendo informações como ID, nome, tipo, status, labels e metadados.
- **`NewServiceDiscovery(...)`**: Função construtora que inicializa o módulo de descoberta de serviços.
- **`Start()` e `Stop()`**: Métodos para iniciar e parar o processo de descoberta, que é executado em background.
- **`runDiscovery()`**: Executa uma rodada de descoberta, chamando os métodos específicos para cada tipo de descoberta (ex: `discoverDockerServices`).

### Fluxo de Operação:

1.  **Inicialização**: O `ServiceDiscovery` é inicializado com as configurações definidas.
2.  **Descoberta Periódica**: O módulo executa um loop em background que, em intervalos configuráveis (`update_interval`), realiza uma nova rodada de descoberta.
3.  **Descoberta Docker**: Se habilitado, o módulo se conecta ao Docker, lista os containers e os filtra com base em labels e nomes para identificar quais devem ser monitorados.
4.  **Descoberta por Arquivo**: Se habilitado, o módulo monitora diretórios e arquivos de configuração para descobrir novos serviços.
5.  **Notificação**: Quando um serviço é adicionado, removido ou atualizado, o `ServiceDiscovery` invoca os callbacks registrados para notificar outros componentes da aplicação (como os monitores).

## Papel e Importância

O módulo `discovery` desempenha um papel **fundamental na automação e na escalabilidade** do `log_capturer_go`. Em vez de configurar manualmente cada fonte de log, este módulo permite que a aplicação descubra e comece a monitorar novos serviços automaticamente.

Isso é especialmente importante em ambientes dinâmicos, como os baseados em containers, onde serviços são criados e destruídos com frequência. A descoberta automática garante que a coleta de logs seja contínua e abrangente, sem a necessidade de intervenção manual.

## Configurações Aplicáveis

As configurações do módulo `discovery` são definidas na seção `discovery` do arquivo `config.yaml`:

- **`enabled`**: Habilita ou desabilita o service discovery.
- **`update_interval`**: Intervalo para executar a descoberta (ex: "60s").
- **`docker_enabled`**: Habilita a descoberta de containers Docker.
- **`docker.socket_path`**: Caminho para o socket do Docker.
- **`docker.required_labels`**: Labels que um container deve ter para ser descoberto.
- **`docker.exclude_labels`**: Labels que, se presentes, fazem com que o container seja ignorado.
- **`file_enabled`**: Habilita a descoberta baseada em arquivos.
- **`file.watch_paths`**: Diretórios para monitorar em busca de arquivos de configuração.

## Problemas e Melhorias

### Problemas Potenciais:

- **Performance**: Em ambientes com um número muito grande de containers, a listagem e filtragem pode consumir recursos significativos.
- **Complexidade de Filtros**: A lógica de filtragem baseada em labels pode se tornar complexa e difícil de gerenciar.
- **Atraso na Descoberta**: O intervalo de atualização (`update_interval`) introduz um atraso entre a criação de um serviço e o início do monitoramento de seus logs.

### Sugestões de Melhorias:

- **Event-Driven Discovery**: Em vez de polling, o módulo poderia ouvir eventos do Docker (ou Kubernetes) para descobrir novos serviços em tempo real, reduzindo a latência.
- **Suporte a Kubernetes**: Implementar a descoberta de serviços em ambientes Kubernetes, utilizando a API do Kubernetes para encontrar pods e containers que devem ser monitorados.
- **Cache de Serviços**: Implementar um cache para os serviços descobertos para otimizar o processo de atualização e reduzir a carga na API do Docker.
- **Validação de Configuração**: Adicionar uma validação mais robusta para as configurações de descoberta para evitar erros em tempo de execução.
