# Módulo: pkg/tenant

## Estrutura

*   `tenant_discovery.go`: Este arquivo contém o componente `TenantDiscovery`, que é responsável por descobrir e gerenciar automaticamente as configurações do tenant.
*   `tenant_manager.go`: Este arquivo contém o `TenantManager`, que é responsável por gerenciar o ciclo de vida dos tenants e seus recursos.

## Como funciona

O módulo `pkg/tenant` fornece uma estrutura para multilocação, permitindo que a aplicação `log_capturer_go` atenda a vários tenants isolados com suas próprias configurações e recursos.

#### `tenant_manager.go`

1.  **Gerenciamento de Tenants (`CreateTenant`, `GetTenant`, `UpdateTenant`, `DeleteTenant`):**
    *   O `TenantManager` fornece um conjunto de métodos para gerenciar o ciclo de vida dos tenants.
    *   `CreateTenant`: Cria um novo tenant com uma determinada configuração.
    *   `GetTenant`: Recupera um tenant por seu ID.
    *   `UpdateTenant`: Atualiza a configuração de um tenant existente.
    *   `DeleteTenant`: Exclui um tenant e seus recursos.

2.  **Isolamento de Tenants:**
    *   Cada tenant tem seu próprio conjunto de recursos, incluindo um dispatcher, processadores, coletores e monitores.
    *   A configuração `isolationMode` determina o nível de isolamento entre os tenants ("soft" ou "hard").

3.  **Roteamento de Logs (`GetTenantForLogEntry`):**
    *   A função `GetTenantForLogEntry` determina qual tenant deve processar uma determinada entrada de log.
    *   Ele pode usar os rótulos da entrada de log ou outros critérios para rotear o log para o tenant apropriado.

#### `tenant_discovery.go`

1.  **Descoberta Automática (`performInitialDiscovery`, `discoverTenantsInPath`):**
    *   O componente `TenantDiscovery` pode descobrir automaticamente as configurações do tenant a partir de um conjunto de caminhos configurados.
    *   Ele pode ler as configurações do tenant de arquivos YAML ou JSON.

2.  **Observação de Arquivos (`watchFileChanges`):**
    *   O componente `TenantDiscovery` pode observar os arquivos de configuração em busca de alterações e atualizar automaticamente os tenants quando uma alteração é detectada.

3.  **Gerenciamento Automático de Tenants:**
    *   O componente `TenantDiscovery` pode ser configurado para criar, atualizar ou excluir automaticamente os tenants com base nos arquivos de configuração descobertos.

## Papel e Importância

O módulo `pkg/tenant` é um componente chave para habilitar a multilocação na aplicação `log_capturer_go`. Seus principais papéis são:

*   **Multilocação:** Permite que a aplicação atenda a vários tenants com suas próprias configurações e recursos isolados.
*   **Escalabilidade:** Facilita o gerenciamento de um grande número de tenants, fornecendo uma maneira centralizada de gerenciar suas configurações e recursos.
*   **Automação:** Os recursos de descoberta e gerenciamento automáticos reduzem a necessidade de intervenção manual ao adicionar, atualizar ou remover tenants.

## Configurações

O módulo `tenant` é configurado através das seções `tenant_management` e `tenant_discovery` do arquivo `config.yaml`. As principais configurações incluem:

*   **Seção `tenant_management`:**
    *   `default_tenant`: O ID do tenant padrão a ser usado quando uma entrada de log não pode ser roteada para um tenant específico.
    *   `isolation_mode`: O nível de isolamento entre os tenants ("soft" ou "hard").
*   **Seção `tenant_discovery`:**
    *   `enabled`: Habilita ou desabilita a descoberta de tenants.
    *   `config_paths`: Uma lista de caminhos para pesquisar arquivos de configuração de tenant.
    *   `auto_create_tenants`, `auto_update_tenants`, `auto_delete_tenants`: Essas configurações controlam se os tenants são criados, atualizados ou excluídos automaticamente com base nos arquivos de configuração descobertos.

## Problemas e Melhorias

*   **Isolamento de Recursos:** A implementação do isolamento de recursos não está totalmente detalhada. Uma implementação mais robusta precisaria usar cgroups ou outros mecanismos para impor limites de recursos para cada tenant.
*   **Validação de Configuração:** A validação da configuração é básica. Um sistema de validação mais abrangente poderia ser implementado para garantir que as configurações do tenant sejam válidas e não entrem em conflito umas com as outras.
*   **UI Específica do Tenant:** O módulo poderia ser aprimorado para fornecer uma UI específica do tenant para gerenciar as configurações do tenant e monitorar os recursos do tenant.
