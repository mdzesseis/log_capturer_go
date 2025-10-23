# Módulo Docker

## Estrutura e Operação

O módulo `docker` é responsável por gerenciar a comunicação com o daemon do Docker. Sua principal função é fornecer uma interface robusta e eficiente para interagir com a API do Docker, utilizando um pool de conexões para otimizar a performance.

### Principais Componentes da Estrutura:

- **`PoolManager`**: Gerencia um pool de clientes Docker. Ele é responsável por criar, manter e distribuir os clientes para os componentes que precisam interagir com o Docker (como o `ContainerMonitor`).
- **`ConnectionPool`**: Uma implementação mais detalhada de um pool de conexões, que gerencia o ciclo de vida das conexões, incluindo a criação, a reutilização e a remoção de conexões ociosas ou não saudáveis.
- **`ClientManager`**: Atua como uma fachada (facade) sobre o pool de conexões, oferecendo métodos convenientes para realizar operações comuns do Docker (como `ContainerList`, `ContainerLogs`, etc.) sem que o chamador precise se preocupar com o gerenciamento da conexão.
- **`PooledClient` / `PooledConnection`**: Estruturas que encapsulam um cliente Docker, juntamente com metadados sobre seu estado (em uso, saudável, tempo de vida, etc.).

### Fluxo de Operação:

1.  **Inicialização**: Um `PoolManager` ou `ConnectionPool` é criado, e um número mínimo de conexões com o Docker daemon é estabelecido.
2.  **Requisição de Conexão**: Quando um componente (como o `ContainerMonitor`) precisa realizar uma operação no Docker, ele solicita uma conexão do pool através do `GetClient()` ou `GetConnection()`.
3.  **Distribuição**: O pool fornece uma conexão saudável e que não esteja em uso. Se todas as conexões estiverem ocupadas e o limite máximo não tiver sido atingido, uma nova conexão pode ser criada.
4.  **Execução da Operação**: O componente utiliza o cliente Docker para executar a operação desejada (ex: listar containers).
5.  **Liberação da Conexão**: Após a conclusão da operação, a conexão é devolvida ao pool para que possa ser reutilizada por outra requisição.
6.  **Health Check**: Periodicamente, o pool verifica a saúde de suas conexões (geralmente enviando um `ping` para o daemon). Conexões que falham no health check são marcadas como não saudáveis e eventualmente substituídas.

## Papel e Importância

O módulo `docker` é **essencial para a funcionalidade de monitoramento de containers**. Ele abstrai a complexidade de gerenciar conexões com o Docker, oferecendo uma interface simples e eficiente.

Sua importância se manifesta em:

- **Performance**: Reutilizar conexões através de um pool é muito mais eficiente do que criar uma nova conexão para cada interação com a API do Docker. Isso reduz a latência e o consumo de recursos.
- **Resiliência**: O mecanismo de health check e a capacidade de substituir conexões não saudáveis tornam a interação com o Docker mais robusta e tolerante a falhas temporárias no daemon.
- **Gerenciamento de Recursos**: Ao limitar o número máximo de conexões, o pool evita que a aplicação sobrecarregue o Docker daemon com um número excessivo de requisições simultâneas.

## Configurações Aplicáveis

As configurações para o pool de conexões Docker são geralmente definidas na seção `container_monitor` do `config.yaml` ou em uma seção dedicada ao pool:

- **`pool_size` / `max_connections`**: O número máximo de conexões a serem mantidas no pool.
- **`min_connections`**: O número mínimo de conexões a serem mantidas ativas.
- **`socket_path`**: O caminho para o socket do Docker (ex: `unix:///var/run/docker.sock`).
- **`health_check_interval`**: A frequência com que a saúde das conexões é verificada.
- **`connection_timeout`**: O tempo máximo de espera para obter uma conexão do pool.
- **`idle_timeout`**: O tempo que uma conexão pode permanecer ociosa antes de ser considerada para remoção.

## Problemas e Melhorias

### Problemas Potenciais:

- **Vazamento de Conexões**: Se um componente pegar uma conexão do pool e não a devolver, o pool pode eventualmente ficar sem conexões disponíveis, causando um bloqueio na aplicação.
- **Configuração do Pool**: Um pool mal configurado (muito pequeno ou muito grande) pode levar a gargalos de performance ou a um consumo excessivo de recursos.
- **Falhas no Docker Daemon**: Se o Docker daemon parar de responder, todas as conexões no pool se tornarão não saudáveis, e a aplicação perderá a capacidade de monitorar os containers até que o daemon se recupere.

### Sugestões de Melhorias:

- **Métricas do Pool**: Expor métricas detalhadas sobre o estado do pool para o Prometheus, como o número de conexões ativas, ociosas, o tempo de espera por uma conexão e a taxa de erros. Isso já está parcialmente implementado, mas pode ser expandido.
- **Estratégia de Balanceamento**: Implementar diferentes estratégias de balanceamento para a distribuição de conexões (além do round-robin), como "pegar a menos usada" (least used).
- **Wrapper para Streams**: A lógica de `pooledReadCloser` é uma boa prática. Garantir que todos os métodos que retornam streams (como `ContainerLogs`) usem um wrapper para devolver a conexão ao pool automaticamente quando o stream for fechado.
- **Detecção de "Stale Connections"**: Melhorar a detecção de conexões que parecem saudáveis (respondem ao ping), mas que estão "presas" em alguma operação, utilizando timeouts mais agressivos nas próprias chamadas da API.
