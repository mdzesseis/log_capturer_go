# Módulo: pkg/discovery

## Estrutura

*   `service_discovery.go`: Este arquivo contém o componente `ServiceDiscovery`, que é responsável por descobrir automaticamente os serviços a serem monitorados.

## Como funciona

O módulo `pkg/discovery` fornece um mecanismo para descobrir e monitorar automaticamente serviços, como contêineres Docker e arquivos de log.

1.  **Inicialização (`NewServiceDiscovery`):**
    *   Cria uma nova instância de `ServiceDiscovery`.
    *   Inicializa um cliente Docker se a descoberta do Docker estiver habilitada.

2.  **Loop de Descoberta (`discoveryLoop`):**
    *   O método `Start` inicia uma goroutine em segundo plano que executa o `discoveryLoop`.
    *   Este loop chama periodicamente `runDiscovery` no intervalo definido na configuração.

3.  **Execução da Descoberta (`runDiscovery`):**
    *   A função `runDiscovery` é responsável por realizar a descoberta real.
    *   Ela chama `discoverDockerServices` se a descoberta do Docker estiver habilitada e `discoverFileServices` se a descoberta de arquivos estiver habilitada.

4.  **Descoberta de Serviço Docker (`discoverDockerServices`):**
    *   Esta função lista todos os contêineres no host Docker.
    *   Para cada contêiner, ela verifica se ele deve ser monitorado com base nos rótulos configurados (`shouldDiscoverContainer`).
    *   Se um contêiner deve ser monitorado, ele cria uma struct `DiscoveredService` com informações sobre o contêiner, como seu nome, imagem e rótulos.
    *   Em seguida, ele chama os callbacks `onServiceAdded`, `onServiceUpdated` ou `onServiceRemoved` para notificar outros componentes da alteração.

5.  **Descoberta de Serviço de Arquivo (`discoverFileServices`):**
    *   Esta função é responsável por descobrir serviços baseados em arquivos. A implementação é atualmente um placeholder.

## Papel e Importância

O módulo `pkg/discovery` é importante para automatizar a configuração da aplicação `log_capturer_go`. Seus principais papéis são:

*   **Automação:** Ele descobre automaticamente novos serviços à medida que são iniciados, eliminando a necessidade de configuração manual.
*   **Configuração Dinâmica:** Permite que a aplicação se adapte às mudanças no ambiente, como quando os contêineres são iniciados, parados ou atualizados.
*   **Escalabilidade:** Facilita o gerenciamento de um grande número de serviços, pois eles podem ser descobertos e monitorados automaticamente.

## Configurações

A seção `service_discovery` do arquivo `config.yaml` é usada para configurar este módulo. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita a descoberta de serviços.
*   `update_interval`: Com que frequência executar o processo de descoberta.
*   `docker_enabled`: Habilita ou desabilita a descoberta de serviços Docker.
*   `file_enabled`: Habilita ou desabilita a descoberta de serviços baseada em arquivos.
*   **Seção `docker`:**
    *   `required_labels`: Um mapa de rótulos que um contêiner deve ter para ser descoberto.
    *   `exclude_labels`: Um mapa de rótulos que fará com que um contêiner seja excluído da descoberta.

## Problemas e Melhorias

*   **Descoberta Baseada em Arquivos:** A descoberta baseada em arquivos ainda não foi implementada. Este seria um recurso útil para descobrir serviços que não estão sendo executados em contêineres Docker.
*   **Descoberta do Kubernetes:** A configuração inclui uma seção para descoberta do Kubernetes, mas a implementação está ausente. Esta seria uma adição valiosa para usuários que executam a aplicação em um ambiente Kubernetes.
*   **Filtragem Mais Sofisticada:** O mecanismo de filtragem atual é baseado em correspondência simples de rótulos. Uma implementação mais avançada poderia suportar regras de filtragem mais complexas, como expressões regulares.
