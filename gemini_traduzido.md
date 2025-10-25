# GEMINI.md

## Visão Geral do Projeto

`ssw-logs-capture` é um sistema de agregação e monit

**Tecnologias principais:**

*   Go
*   Docker
*   Prometheus (para métricas)
*   Loki (como destino de logs)
*   Elasticsearch (como destino de logs)
*   Splunk (como destino de logs)

**Arquitetura:**

O projeto segue uma arquitetura modular:

*   **`cmd/main.go`**: O ponto de entrada principal da aplicação.
*   **`internal/app/app.go`**: Contém a lógica central da aplicação, incluindo a orquestração de componentes e o servidor HTTP para APIs.
*   **`internal/config/`**: Lida com o carregamento e a validação de configurações baseadas em YAML.
*   **`internal/dispatcher/`**: Gerencia o roteamento de logs, formação de lotes (batching) e pools de workers para processamento eficiente.
*   **`internal/monitors/`**: Implementa a lógica para monitorar sistemas de arquivos e contêineres Docker em busca de novas entradas de log.
*   **`internal/processing/`**: Define os pipelines de processamento de logs, permitindo transformação e enriquecimento dos logs.
*   **`internal/sinks/`**: Contém implementações para diversos destinos de saída de logs.
*   **`pkg/`**: Uma coleção de pacotes utilitários que fornecem funcionalidades como circuit breakers, compactação, rastreamento de posição de arquivos e mais.

## Compilação e Execução

### Desenvolvimento local

Um script auxiliar é fornecido para tarefas comuns de desenvolvimento:

*   **Build:** `scripts_go/dev.sh build`
*   **Run:** `scripts_go/dev.sh run`
*   **Test:** `scripts_go/dev.sh test`
*   **Format:** `scripts_go/dev.sh fmt`
*   **Lint:** `scripts_go/dev.sh lint`

Alternativamente, você pode usar os comandos padrão do Go:

*   **Build:** `go build -o ssw-logs-capture ./cmd/main.go`
*   **Run:** `go run ./cmd/main.go`
*   **Executar com configuração personalizada:** `go run ./cmd/main.go -config configs/custom.yaml`
*   **Executar testes:** `go test ./...`

### Desenvolvimento com Docker

O projeto inclui um arquivo `docker-compose.yml` para um ambiente de desenvolvimento baseado em Docker.

*   **Construir e executar:** `docker-compose up --build`
*   **Ver logs:** `docker-compose logs -f log_capturer_go`
*   **Parar:** `docker-compose down`

## Convenções de Desenvolvimento

*   **Configuração:** A configuração é gerenciada por meio de arquivos YAML localizados no diretório `configs/`.
    *   `configs/config.yaml`: Configuração principal da aplicação.
    *   `configs/pipelines.yaml`: Regras de processamento de logs.
    *   `configs/file_pipeline.yml`: Regras específicas de monitoramento de arquivos.
*   **Testes:** Um script abrangente de execução de testes está disponível em `test-scripts/test-runner.sh`.
    *   Executar todos os testes: `./test-scripts/test-runner.sh all`
    *   Executar testes unitários: `./test-scripts/test-runner.sh unit`
*   **API:** A aplicação expõe um conjunto de APIs RESTful para monitoramento e gerenciamento. Consulte `API.md` para detalhes.
