# Módulo Config

## Estrutura e Operação

O módulo `config` é responsável por carregar, validar e gerenciar todas as configurações do `log_capturer_go`. Ele centraliza a lógica de configuração, permitindo que a aplicação seja flexível e fácil de configurar.

### Principais Componentes da Estrutura:

- **`LoadConfig(configFile string)`**: A função principal do módulo, que lê um arquivo de configuração YAML, aplica valores padrão e pode (opcionalmente) sobrescrever configurações com variáveis de ambiente.
- **`applyDefaults(config *types.Config)`**: Garante que a aplicação tenha valores padrão para configurações essenciais, evitando falhas se alguma configuração estiver ausente.
- **`ValidateConfig(config *types.Config)`**: Realiza uma validação completa da configuração carregada, verificando se os valores são válidos e se as dependências entre as configurações estão corretas.
- **`ConfigValidator` Struct**: Uma estrutura que encapsula a lógica de validação, permitindo a verificação de diferentes seções da configuração (app, server, sinks, etc.) de forma organizada.

### Fluxo de Operação:

1.  **Carregamento**: A aplicação chama `LoadConfig` com o caminho para o arquivo `config.yaml`.
2.  **Parsing**: O arquivo YAML é lido e desserializado para a estrutura `types.Config`.
3.  **Valores Padrão**: A função `applyDefaults` preenche quaisquer campos de configuração que não foram especificados no arquivo.
4.  **Validação**: `ValidateConfig` é chamado para garantir que a configuração seja válida. Se houver erros, a aplicação não inicia.
5.  **Disponibilização**: A configuração validada é retornada para o módulo `app`, que a utiliza para inicializar todos os outros componentes.

## Papel e Importância

O módulo `config` é **essencial para a flexibilidade e a manutenibilidade** do `log_capturer_go`. Ele desacopla a lógica da aplicação de suas configurações, permitindo que o comportamento do sistema seja alterado sem a necessidade de modificar o código.

Sua importância reside em:

- **Centralização**: Todas as configurações estão em um único lugar, facilitando o gerenciamento.
- **Validação**: Garante que a aplicação não inicie com uma configuração inválida, prevenindo erros em tempo de execução.
- **Flexibilidade**: Permite que a aplicação se adapte a diferentes ambientes (desenvolvimento, produção) e casos de uso através de um arquivo de configuração.

## Configurações Aplicáveis

Este módulo gerencia **todas as configurações** da aplicação, que são definidas no arquivo `config.yaml`. As principais seções de configuração incluem:

- **`app`**: Configurações gerais da aplicação (nome, versão, nível de log).
- **`server`**: Configurações do servidor HTTP.
- **`metrics`**: Configurações do servidor de métricas Prometheus.
- **`dispatcher`**: Configurações do despachante de logs (tamanho da fila, workers).
- **`monitors`**: Configurações para os monitores de arquivos e containers.
- **`sinks`**: Configurações para os destinos dos logs (Loki, Elasticsearch, etc.).
- **`positions`**: Configurações para o gerenciamento de posições de leitura.

## Problemas e Melhorias

### Problemas Potenciais:

- **Complexidade do YAML**: O arquivo de configuração pode se tornar muito grande e complexo, dificultando o entendimento e a manutenção.
- **Validação Incompleta**: Se a lógica de validação não for abrangente, configurações inválidas podem passar despercebidas e causar problemas em tempo de execução.
- **Segredos no Arquivo**: Armazenar senhas e tokens diretamente no arquivo de configuração é uma má prática de segurança.

### Sugestões de Melhorias:

- **Gerenciamento de Segredos**: Integrar com um sistema de gerenciamento de segredos (como HashiCorp Vault ou variáveis de ambiente) para carregar informações sensíveis de forma segura.
- **Hot-Reloading**: Implementar um mecanismo de hot-reloading para que as configurações possam ser alteradas sem a necessidade de reiniciar a aplicação.
- **Validação com Schemas**: Utilizar JSON Schema ou uma ferramenta similar para validar a estrutura do arquivo de configuração de forma mais robusta.
- **Modularização da Configuração**: Dividir a configuração em múltiplos arquivos (um para cada módulo principal) para melhorar a organização e a legibilidade.
