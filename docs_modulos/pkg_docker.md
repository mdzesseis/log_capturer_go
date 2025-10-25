# Módulo: pkg/docker

## Estrutura

*   `client_manager.go`: Este arquivo contém o `ClientManager`, que fornece uma interface de alto nível para interagir com o daemon do Docker, usando um pool de conexões.
*   `connection_pool.go`: Este arquivo contém o `ConnectionPool`, que gerencia um pool de conexões de cliente Docker.
*   `pool_manager.go`: Este arquivo contém o `PoolManager`, que é outra implementação de um pool de clientes Docker.

## Como funciona

O módulo `pkg/docker` é responsável por gerenciar as conexões com o daemon do Docker e fornecer uma maneira confiável de interagir com a API do Docker.

1.  **Pooling de Conexões (`connection_pool.go` e `pool_manager.go`):**
    *   O módulo implementa dois gerenciadores de pool de conexões diferentes, `ConnectionPool` e `PoolManager`. Ambos servem a um propósito semelhante: manter um pool de conexões de cliente Docker reutilizáveis.
    *   Isso é importante para o desempenho, pois criar uma nova conexão com o daemon do Docker para cada solicitação pode ser caro.
    *   Os pools lidam com a criação, gerenciamento e fechamento de conexões. Eles também realizam verificações de saúde para garantir que as conexões ainda sejam válidas.

2.  **Gerenciador de Clientes (`client_manager.go`):**
    *   O `ClientManager` fornece uma abstração de nível superior sobre o pool de conexões.
    *   Possui métodos para operações comuns do Docker, como `ContainerList`, `ContainerInspect` e `ContainerLogs`.
    *   Quando um desses métodos é chamado, o `ClientManager` obtém uma conexão do pool, executa a operação solicitada e, em seguida, retorna a conexão ao pool.

3.  **Verificações de Saúde:**
    *   Os pools de conexões executam periodicamente verificações de saúde nas conexões para garantir que ainda estejam ativas e responsivas.
    *   Se uma conexão for considerada insalubre, ela será removida do pool e substituída por uma nova.

## Papel e Importância

O módulo `pkg/docker` é um componente crítico para o `ContainerMonitor`. Seus principais papéis são:

*   **Interação com a API do Docker:** Fornece a funcionalidade necessária para interagir com a API do Docker, o que é essencial para monitorar os logs dos contêineres.
*   **Desempenho:** O mecanismo de pooling de conexões melhora o desempenho ao reutilizar as conexões existentes com o daemon do Docker.
*   **Confiabilidade:** As verificações de saúde e a lógica de nova tentativa ajudam a garantir que a aplicação possa se conectar de forma confiável ao daemon do Docker, mesmo que haja problemas de rede transitórios.

## Configurações

O módulo `docker` é configurado através da seção `container_monitor` do arquivo `config.yaml`. As principais configurações incluem:

*   `socket_path`: O caminho para o soquete do Docker.

A própria configuração do pool de conexões (`PoolConfig`) inclui configurações para o tamanho do pool, tempos limite e intervalo de verificação de saúde.

## Problemas e Melhorias

*   **Redundância:** Existem duas implementações diferentes de um pool de clientes Docker (`ConnectionPool` e `PoolManager`). Isso é redundante e pode ser confuso. O código deve ser refatorado para usar uma única implementação bem testada.
*   **Tratamento de Erros:** O tratamento de erros poderia ser melhorado. Por exemplo, o `ClientManager` poderia implementar uma lógica de nova tentativa mais sofisticada para chamadas à API do Docker que falham devido a erros transitórios.
*   **Configuração:** A configuração do pool de conexões não está diretamente exposta no arquivo `config.yaml` principal. Seria benéfico permitir que os usuários ajustassem as configurações do pool de conexões a partir do arquivo de configuração principal.
