# Módulo: internal/config

## Estrutura

*   `config.go`: Este arquivo contém toda a lógica para carregar, analisar e validar a configuração da aplicação.

## Como funciona

O módulo `internal/config` é responsável por gerenciar a configuração da aplicação.

1.  **Carregando a Configuração (função `LoadConfig`):**
    *   Começa inicializando uma struct `types.Config` com valores padrão.
    *   Se um arquivo de configuração é especificado (ex: `configs/config.yaml`), ele carrega as configurações daquele arquivo, sobrescrevendo os padrões.
    *   Em seguida, aplica sobrescritas de variáveis de ambiente, que têm a maior precedência.
    *   Ele também carrega um `file_pipeline.yml` separado, se especificado.

2.  **Aplicando Padrões (função `applyDefaults`):**
    *   Esta função define valores padrão sensatos para todos os parâmetros de configuração. Isso garante que a aplicação possa ser executada mesmo com um arquivo de configuração mínimo.

3.  **Aplicando Sobrescritas de Ambiente (função `applyEnvironmentOverrides`):**
    *   Esta função verifica as variáveis de ambiente que correspondem às configurações (ex: `SSW_LOKI_URL`) e usa seus valores para sobrescrever quaisquer configurações do arquivo de configuração.

4.  **Validação (`ValidateConfig` e `ConfigValidator`):**
    *   Após carregar a configuração, ele realiza uma validação abrangente de todas as configurações.
    *   A struct `ConfigValidator` possui uma série de métodos de validação (`validateApp`, `validateServer`, etc.) que verificam coisas como:
        *   Valores vazios ou inválidos.
        *   Portas dentro do intervalo válido.
        *   Caminhos de arquivo acessíveis e com as permissões corretas.
        *   Nenhuma configuração conflitante (ex: dois servidores na mesma porta).

## Papel e Importância

O módulo `internal/config` é crucial para a flexibilidade e robustez da aplicação. Seus papéis principais são:

*   **Configuração Centralizada:** Fornece uma maneira única e consistente de configurar todos os aspectos da aplicação.
*   **Flexibilidade:** Ao suportar arquivos YAML и variáveis de ambiente, permite uma configuração fácil em diferentes ambientes (desenvolvimento, homologação, produção).
*   **Robustez:** Os valores padrão e a validação garantem que a aplicação inicie com uma configuração válida e funcional, prevenindo erros comuns.
*   **Manutenibilidade:** Desacopla a lógica de configuração dos componentes que a utilizam, tornando o código mais limpo e fácil de manter.

## Configurações

Este módulo é o coração da configuração da aplicação. Ele gerencia todas as configurações definidas em `configs/config.yaml`, `configs/enterprise-config.yaml` e `configs/file_pipeline.yml`. Ele também lida com sobrescritas de variáveis de ambiente.

## Problemas e Melhorias

*   **Incompatibilidade de Tipos:** O código possui uma seção temporariamente desabilitada em `loadFilePipeline` devido a uma incompatibilidade de tipos. Isso deve ser corrigido.
*   **Validação Complexa:** O `ConfigValidator` é bastante grande. Ele poderia ser dividido em validadores menores e mais focados para cada componente.
*   **Gerenciamento de Segredos:** A implementação atual carrega segredos diretamente do arquivo de configuração ou de variáveis de ambiente. Uma abordagem mais segura seria integrar com um sistema de gerenciamento de segredos como HashiCorp Vault ou AWS Secrets Manager.
*   **Recarregamento Dinâmico:** Embora exista um módulo `hotreload`, o próprio módulo `config` não parece ter nenhuma lógica específica para suportar o recarregamento dinâmico da configuração em tempo de execução. Esta poderia ser uma adição valiosa.
