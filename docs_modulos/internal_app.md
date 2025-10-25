# Módulo: internal/app

## Estrutura

*   `app.go`: O núcleo da aplicação, contendo a struct `App` que orquestra todos os componentes.
*   `initialization.go`: Contém a lógica para inicializar todos os componentes da aplicação.
*   `handlers.go`: Define os manipuladores HTTP para os endpoints da API da aplicação.
*   `utils.go`: Contém funções utilitárias usadas dentro do pacote `app`.
*   `app_test.go`: Contém testes unitários e de integração para o pacote `app`.

## Como funciona

O módulo `internal/app` é o ponto de entrada principal e orquestrador da aplicação `log_capturer_go`.

1.  **Inicialização (função `New` em `app.go`):**
    *   Carrega e valida a configuração de um arquivo YAML (`configs/config.yaml`).
    *   Configura um logger estruturado (Logrus).
    *   Inicializa todos os componentes principais chamando `initializeComponents`.

2.  **Inicialização de Componentes (`initializeComponents` e `initialization.go`):**
    *   O processo de inicialização é sequencial para garantir que as dependências sejam atendidas.
    *   **Serviços Principais:** `TaskManager`, `LogProcessor` e `Dispatcher` são inicializados primeiro.
    *   **Sinks:** Destinos de saída como Loki e arquivos locais são configurados.
    *   **Monitores:** `FileMonitor` e `ContainerMonitor` são configurados para capturar logs.
    *   **Serviços Auxiliares:** `PositionManager`, `DiskManager`, `ResourceMonitor`, `DiskBuffer`, `AnomalyDetector` e `EnhancedMetrics` são inicializados.
    *   **Recursos Empresariais:** `SecurityManager`, `TracingManager`, `SLOManager` e `GoroutineTracker` são configurados se habilitados.
    *   **Servidores HTTP:** O servidor da API principal e o servidor de métricas do Prometheus são configurados.

3.  **Executando a Aplicação (função `Run` em `app.go`):**
    *   O método `Run` chama `Start` para iniciar o ciclo de vida de todos os componentes inicializados.
    *   Em seguida, ele escuta por sinais de desligamento (SIGINT, SIGTERM) para acionar um desligamento gracioso chamando o método `Stop`.

4.  **Manipuladores HTTP (`handlers.go`):**
    *   O módulo expõe vários endpoints HTTP para monitoramento e gerenciamento, incluindo:
        *   `/health`: Fornece o status de saúde da aplicação e seus componentes.
        *   `/stats`: Oferece estatísticas operacionais detalhadas.
        *   `/config`: Mostra a configuração atual (sanitizada).
        *   `/metrics`: Expõe métricas do Prometheus.
        *   `/debug/*`: Endpoints para depuração de goroutines, memória, etc.

## Papel e Importância

O módulo `internal/app` é o coração da aplicação. Ele é responsável por:

*   **Orquestração:** Ele une todos os diferentes módulos e componentes, gerenciando seu ciclo de vida e garantindo que funcionem em harmonia.
*   **Configuração:** É o ponto central para carregar e gerenciar a configuração da aplicação.
*   **Gerenciamento do Ciclo de Vida:** Lida com o início e o desligamento gracioso de toda a aplicação.
*   **Exposição da API:** Fornece a interface externa para monitorar e interagir com a aplicação através de sua API HTTP.

Sem este módulo, a aplicação `log_capturer_go` seria uma coleção de componentes desconectados.

## Configurações

O módulo `internal/app` é configurado através do arquivo `config.yaml`. As seções principais relacionadas a este módulo são:

*   **`app`:** Configurações gerais da aplicação como nome, versão, nível de log e formato de log.
*   **`server`:** Configuração para o servidor da API HTTP principal, incluindo porta, host e timeouts.
*   **`metrics`:** Configurações para o servidor de métricas do Prometheus.

O módulo também usa indiretamente a configuração de todos os componentes que ele inicializa.

## Problemas e Melhorias

*   **Acoplamento Forte:** A struct `App` está fortemente acoplada a todos os componentes que gerencia. Isso pode dificultar os testes e a manutenção. Uma arquitetura mais desacoplada, talvez usando injeção de dependência, poderia ser benéfica.
*   **Complexidade da Inicialização:** A função `initializeComponents` é longa e complexa. Ela poderia ser dividida em funções menores e mais gerenciáveis.
*   **Gerenciamento de Configuração:** O carregamento e a validação da configuração poderiam ser mais robustos, com melhor tratamento de erros e regras de validação mais detalhadas.
*   **Testes:** Embora existam testes, o arquivo `app_test.go` poderia ser expandido para cobrir mais cenários de integração e casos extremos.
