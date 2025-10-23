# Módulo App

## Estrutura e Operação

O módulo `app` é o núcleo do sistema `log_capturer_go`, responsável pela orquestração e gerenciamento de todos os outros componentes. Sua estrutura é centrada no `App`, que encapsula a lógica principal da aplicação.

### Principais Componentes da Estrutura:

- **`App` Struct**: A estrutura principal que contém todas as dependências, configurações e o estado da aplicação. Ela gerencia o ciclo de vida de todos os componentes, desde a inicialização até o desligamento.
- **`New(configFile string)`**: Função construtora que inicializa uma nova instância da aplicação. Carrega e valida as configurações, configura o logger e inicializa todos os componentes.
- **`initializeComponents()`**: Orquestra a inicialização de todos os módulos em uma ordem específica para garantir que as dependências sejam satisfeitas.
- **`Start()`**: Inicia todos os componentes da aplicação, como os monitores de logs, o dispatcher e os sinks.
- **`Stop()`**: Realiza o desligamento gradual de todos os componentes, garantindo que não haja perda de dados.
- **`Run()`**: Executa a aplicação, aguardando por sinais de interrupção (SIGINT, SIGTERM) para iniciar o processo de desligamento.
- **Handlers HTTP**: O módulo também define handlers para endpoints HTTP que expõem o estado da aplicação, como `/health`, `/stats` e `/config`.

### Fluxo de Operação:

1.  **Inicialização**: A aplicação é iniciada através da função `New`, que carrega o arquivo de configuração.
2.  **Orquestração**: O `App` inicializa e gerencia o ciclo de vida de todos os componentes, incluindo:
    - **Monitors**: `file_monitor` e `container_monitor` para coletar logs.
    - **Dispatcher**: Para orquestrar o processamento e envio de logs.
    - **Sinks**: Para enviar os logs a destinos como Loki, Elasticsearch, etc.
    - **Serviços Auxiliares**: Como `PositionManager` para rastrear posições de leitura e `TaskManager` para gerenciar tarefas em segundo plano.
3.  **Execução**: A aplicação entra em um loop de execução, aguardando sinais de desligamento.
4.  **Desligamento**: Ao receber um sinal, o `Stop()` é chamado para um desligamento seguro e ordenado.

## Papel e Importância

O módulo `app` é de **extrema importância** por ser o ponto central de controle da aplicação. Ele garante que todos os componentes funcionem em harmonia, gerenciando o fluxo de dados e o ciclo de vida de cada parte do sistema. Sem ele, os módulos seriam apenas peças isoladas sem uma coordenação central.

Sua principal responsabilidade é garantir a **orquestração e a integração** de todos os outros módulos, tornando o sistema coeso e funcional.

## Configurações Aplicáveis

O módulo `app` é configurado principalmente através do arquivo `config.yaml`. As configurações mais relevantes para este módulo incluem:

- **`app.log_level`**: Define o nível de log da aplicação (ex: "info", "debug").
- **`app.log_format`**: Formato do log (ex: "json", "text").
- **`server.enabled`**: Habilita ou desabilita o servidor HTTP para endpoints de monitoramento.
- **`server.host`** e **`server.port`**: Endereço e porta para o servidor HTTP.
- **`metrics.enabled`** e **`metrics.port`**: Configurações para o servidor de métricas Prometheus.

Além disso, as configurações de todos os outros módulos (como `dispatcher`, `sinks`, `monitors`) são carregadas e gerenciadas pelo `app`.

## Problemas e Melhorias

### Problemas Potenciais:

- **Complexidade**: Por ser o orquestrador central, o módulo `app` pode se tornar complexo à medida que novas funcionalidades são adicionadas.
- **Dependências Cíclicas**: Um cuidado especial deve ser tomado para evitar dependências cíclicas entre o `app` e os módulos que ele gerencia.
- **Ordem de Inicialização**: A ordem de inicialização dos componentes é crítica. Um erro nessa ordem pode causar falhas em cascata.

### Sugestões de Melhorias:

- **Injeção de Dependência**: Utilizar um framework de injeção de dependência para gerenciar a criação e o ciclo de vida dos componentes, reduzindo o acoplamento.
- **Componentes como Plugins**: Refatorar a inicialização para tratar os componentes como "plugins", permitindo que sejam adicionados ou removidos com mais facilidade.
- **Testes de Integração**: Aumentar a cobertura de testes de integração para garantir que a orquestração entre os diferentes componentes funcione como esperado em diversos cenários.
- **Configuração Dinâmica**: Melhorar o suporte a hot-reloading para permitir que mais configurações sejam alteradas em tempo de execução sem a necessidade de reiniciar a aplicação.
