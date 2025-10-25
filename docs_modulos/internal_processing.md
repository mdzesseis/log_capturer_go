# Módulo: internal/processing

## Estrutura

*   `log_processor.go`: Este arquivo contém o `LogProcessor` e a lógica para processar entradas de log através de pipelines configuráveis.

## Como funciona

O módulo `internal/processing` é responsável por transformar e enriquecer as entradas de log à medida que passam pelo sistema.

1.  **Inicialização (`NewLogProcessor`):**
    *   Cria um novo `LogProcessor`.
    *   Carrega e compila os pipelines de processamento de um arquivo YAML (ex: `configs/pipelines.yaml`).

2.  **Carregamento e Compilação de Pipeline (`loadPipelines`, `compilePipeline`, `compileStep`):**
    *   A função `loadPipelines` lê o arquivo de configuração do pipeline.
    *   Para cada pipeline definido no arquivo, ele compila os passos em processadores executáveis.
    *   A função `compileStep` cria uma instância de um `StepProcessor` com base no `type` do passo (ex: `regex_extract`, `timestamp_parse`).

3.  **Processamento de Log (`Process`):**
    *   Este é o principal ponto de entrada para uma entrada de log no módulo de processamento.
    *   Primeiro, ele encontra o pipeline apropriado para a entrada de log com base no `source_mapping` na configuração (`findPipeline`).
    *   Em seguida, ele processa a entrada de log através de cada passo do pipeline selecionado (`processThroughPipeline`).
    *   Cada passo pode modificar a entrada de log adicionando, removendo ou alterando campos e rótulos.

4.  **Processadores de Passo:**
    *   O módulo define vários tipos de processadores de passo, cada um com uma função específica:
        *   `RegexExtractProcessor`: Extrai dados da mensagem de log usando expressões regulares.
        *   `TimestampParseProcessor`: Analisa timestamps da mensagem de log.
        *   `JSONParseProcessor`: Analisa JSON da mensagem de log.
        *   `FieldAddProcessor`: Adiciona novos campos ou rótulos à entrada de log.
        *   `FieldRemoveProcessor`: Remove campos ou rótulos da entrada de log.
        *   `LogLevelExtractProcessor`: Extrai o nível de log da mensagem.

## Papel e Importância

O módulo `processing` é um componente chave para adicionar estrutura e contexto aos dados de log brutos. Seus principais papéis são:

*   **Enriquecimento de Dados:** Adiciona metadados valiosos aos logs, como timestamps analisados, níveis de log e campos extraídos.
*   **Normalização:** Pode ser usado para normalizar formatos de log de diferentes fontes em uma estrutura consistente.
*   **Flexibilidade:** A abordagem baseada em pipeline permite um processamento de log altamente flexível e personalizável.
*   **Roteamento:** O `source_mapping` permite que diferentes pipelines de processamento sejam aplicados a diferentes fontes de log.

## Configurações

O módulo `processing` é configurado através das seções `processing` e `pipelines` dos arquivos `config.yaml` e `pipelines.yaml`.

*   **Seção `processing` em `config.yaml`:**
    *   `enabled`: Habilita ou desabilita o módulo de processamento.
    *   `pipelines_file`: O caminho para o arquivo de configuração do pipeline.
*   **`pipelines.yaml`:**
    *   `pipelines`: Uma lista de pipelines de processamento, cada um com um nome, descrição e uma série de passos.
    *   `source_mapping`: Um mapeamento de fontes de log para pipelines.

## Problemas e Melhorias

*   **Tratamento de Erros:** O tratamento de erros na função `Process` poderia ser mais robusto. Atualmente, se um passo falhar, todo o processamento para aquela entrada de log é abortado. Uma abordagem mais resiliente poderia ser pular o passo com falha e continuar com o resto do pipeline.
*   **Mais Processadores:** O módulo poderia ser estendido com mais tipos de processadores, como:
    *   Um processador para buscar dados de fontes externas (ex: um banco de dados ou API).
    *   Um processador para realizar cálculos em campos numéricos.
    *   Um processador para redigir informações sensíveis.
*   **Testes:** O módulo se beneficiaria de testes unitários mais abrangentes para cada tipo de processador e para a lógica do pipeline em si.
*   **Recarregamento Dinâmico de Pipeline:** Os pipelines são atualmente carregados na inicialização. Uma grande melhoria seria permitir o recarregamento dinâmico da configuração do pipeline sem reiniciar a aplicação.
