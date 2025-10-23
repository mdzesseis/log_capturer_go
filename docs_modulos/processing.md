# Módulo Processing

## Estrutura e Operação

O módulo `processing` é responsável por transformar e enriquecer os logs brutos através de um sistema de pipelines configuráveis. Ele permite a extração de dados estruturados, a manipulação de campos e a normalização dos logs antes de serem enviados aos `sinks`.

### Principais Componentes da Estrutura:

- **`LogProcessor`**: A estrutura principal que carrega e gerencia os pipelines de processamento.
- **`Pipeline`**: Representa um pipeline de processamento, que consiste em uma sequência de `ProcessingStep`.
- **`ProcessingStep`**: Define um passo individual no pipeline, como `regex_extract`, `timestamp_parse`, `json_parse`, etc.
- **`StepProcessor`**: Uma interface que define o comportamento de um processador de passo. Cada tipo de passo (ex: `RegexExtractProcessor`) implementa esta interface.

### Fluxo de Operação:

1.  **Carregamento de Pipelines**: O `LogProcessor` carrega a configuração dos pipelines a partir de um arquivo YAML (ex: `pipelines.yaml`).
2.  **Compilação**: Cada pipeline e seus respectivos passos são "compilados", o que significa que as configurações são validadas e os processadores de passo são instanciados.
3.  **Seleção de Pipeline**: Para cada log que chega, o `LogProcessor` determina qual pipeline deve ser aplicado com base em um `sourceMapping` que associa fontes de log a pipelines específicos.
4.  **Execução do Pipeline**: O log passa por cada `ProcessingStep` do pipeline selecionado. Cada passo pode modificar o log, extraindo campos, adicionando labels, parseando timestamps, etc.
5.  **Condicionais**: Os passos podem ter condições (`condition`) que determinam se devem ser executados com base no conteúdo do log.

## Papel e Importância

O módulo `processing` é **fundamental para transformar logs brutos e não estruturados em dados ricos e pesquisáveis**. Ele agrega valor aos logs, tornando-os mais úteis para análise, monitoramento e alertas.

Sua importância reside em:

- **Estruturação de Dados**: Extrai informações valiosas de logs de texto simples e as converte em campos estruturados (labels).
- **Normalização**: Padroniza os logs, por exemplo, parseando timestamps de diferentes formatos para um formato unificado.
- **Enriquecimento**: Adiciona contexto aos logs, como o nível de log (info, error) ou outros metadados relevantes.
- **Flexibilidade**: Permite que os usuários definam lógicas de processamento personalizadas para diferentes tipos de logs sem alterar o código da aplicação.

## Configurações Aplicáveis

As configurações de processamento são definidas em um arquivo YAML separado, geralmente `pipelines.yaml`, e são referenciadas na seção `processing` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita o processamento de logs.
- **`pipelines_file`**: O caminho para o arquivo que define os pipelines.

Dentro do arquivo de pipelines, cada pipeline pode ter uma série de passos, cada um com sua própria configuração. Por exemplo, um passo `regex_extract` teria as seguintes configurações:

- **`type: regex_extract`**
- **`config.pattern`**: A expressão regular a ser usada.
- **`config.fields`**: Os nomes dos campos a serem criados a partir dos grupos de captura da regex.

## Problemas e Melhorias

### Problemas Potenciais:

- **Performance de Regex**: Expressões regulares complexas ou ineficientes podem degradar significativamente a performance do processamento.
- **Complexidade dos Pipelines**: Pipelines com muitos passos e condições podem se tornar difíceis de entender, depurar e manter.
- **Ordenação dos Passos**: A ordem dos passos em um pipeline é crucial e uma ordenação incorreta pode levar a resultados inesperados.

### Sugestões de Melhorias:

- **Validadores de Pipeline**: Criar uma ferramenta para validar a sintaxe e a lógica dos pipelines antes de carregá-los, ajudando a prevenir erros.
- **Processadores de Passo Adicionais**: Adicionar mais tipos de processadores, como um para `grok patterns`, um para `user-agent parsing` ou um para `geolocation` a partir de IPs.
- **Testes de Pipeline**: Implementar um mecanismo que permita testar os pipelines com exemplos de logs para verificar se o resultado é o esperado.
- **Interface Gráfica**: Desenvolver uma interface gráfica para criar e gerenciar os pipelines de forma mais visual e intuitiva.
