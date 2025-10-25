# Módulo: pkg/types

## Estrutura

*   `types.go`: Este arquivo define as estruturas de dados principais usadas em toda a aplicação, como `LogEntry` e `ProcessingStep`.
*   `config.go`: Este arquivo define as estruturas de dados para a configuração da aplicação, que são usadas para analisar o arquivo `config.yaml`.
*   `enterprise.go`: Este arquivo define as estruturas de dados para os recursos empresariais, como segurança, rastreamento e monitoramento de SLO.
*   `interfaces.go`: Este arquivo define as interfaces para os componentes conectáveis da aplicação, como `Monitor`, `Sink`, `Dispatcher` e `Processor`.
*   `statistics.go`: Este arquivo define as estruturas de dados para as estatísticas e o monitoramento da aplicação.

## Como funciona

O módulo `pkg/types` é a base da aplicação `log_capturer_go`. Ele define as estruturas de dados e as interfaces que são usadas por todos os outros componentes da aplicação.

*   **`LogEntry`:** Esta é a estrutura de dados central que representa uma única entrada de log. Ela contém a mensagem de log, o timestamp, os rótulos e outros metadados.
*   **Structs de Configuração:** Essas structs são usadas para analisar o arquivo `config.yaml` e para configurar os diferentes componentes da aplicação.
*   **Interfaces:** As interfaces definem os contratos para os componentes conectáveis da aplicação. Isso permite que diferentes implementações de monitores, coletores e processadores sejam usadas sem alterar a lógica principal da aplicação.
*   **Structs de Estatísticas:** Essas structs são usadas para coletar e expor estatísticas sobre o desempenho e a saúde da aplicação.

## Papel e Importância

O módulo `pkg/types` é um componente crítico da aplicação `log_capturer_go`. Seus principais papéis são:

*   **Modelagem de Dados:** Ele define o modelo de dados para a aplicação, o que é essencial para garantir que todos os componentes da aplicação possam trabalhar juntos.
*   **Extensibilidade:** O uso de interfaces torna a aplicação extensível, pois novos monitores, coletores e processadores podem ser adicionados sem alterar a lógica principal da aplicação.
*   **Configuração:** As structs de configuração fornecem uma maneira estruturada e segura de configurar a aplicação.
*   **Observabilidade:** As structs de estatísticas fornecem os dados necessários para o monitoramento e a observabilidade.

## Configurações

Este módulo não possui nenhuma configuração específica no arquivo `config.yaml`. Ele define as estruturas de dados que são usadas para analisar o arquivo de configuração.

## Problemas e Melhorias

*   **Geração de Código:** As structs de configuração e estatísticas poderiam ser geradas a partir de uma definição mais abstrata, como uma especificação de Protocol Buffers ou OpenAPI. Isso tornaria mais fácil manter as diferentes ligações de linguagem em sincronia e gerar documentação.
*   **Versionamento:** As estruturas de dados poderiam ser versionadas para facilitar o tratamento da compatibilidade com versões anteriores ao fazer alterações no modelo de dados.
*   **Tipos Personalizados:** A struct `LogEntry` poderia ser tornada mais extensível, permitindo que campos personalizados sejam adicionados sem alterar a estrutura de dados principal.
