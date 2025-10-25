# Módulo: pkg/hotreload

## Estrutura

*   `config_reloader.go`: Este arquivo contém o `ConfigReloader`, que é responsável por recarregar automaticamente a configuração da aplicação quando ela muda.

## Como funciona

O módulo `pkg/hotreload` fornece um mecanismo para detectar automaticamente alterações nos arquivos de configuração e recarregá-los sem reiniciar a aplicação.

1.  **Inicialização (`NewConfigReloader`):**
    *   Cria uma nova instância de `ConfigReloader`.
    *   Inicializa um `fsnotify.Watcher` para monitorar os arquivos de configuração em busca de alterações.
    *   Calcula o hash inicial do arquivo de configuração para rastrear as alterações.

2.  **Observação de Arquivos (`watchFileChanges`):**
    *   O método `Start` inicia uma goroutine em segundo plano que executa o loop `watchFileChanges`.
    *   Este loop escuta eventos do sistema de arquivos (por exemplo, escrita, criação, renomeação) nos arquivos de configuração definidos.
    *   Quando uma alteração é detectada, ele usa um temporizador de debounce para aguardar um curto período antes de acionar uma recarga. Isso evita que várias recargas sejam acionadas em rápida sucessão.

3.  **Verificação Periódica (`periodicCheck`):**
    *   Além de observar os eventos do sistema de arquivos, o `ConfigReloader` também verifica periodicamente se há alterações, comparando o hash atual do arquivo de configuração com o último hash conhecido. Isso fornece um mecanismo de fallback caso os eventos do sistema de arquivos sejam perdidos.

4.  **Recarregamento (`performReload`):**
    *   Quando uma alteração é detectada, a função `performReload` é chamada.
    *   Primeiro, ele faz backup da configuração atual se `BackupOnReload` estiver habilitado.
    *   Em seguida, ele carrega a nova configuração usando a função `config.LoadConfig`.
    *   Se `ValidateOnReload` estiver habilitado, ele valida a nova configuração antes de aplicá-la.
    *   Se a nova configuração for válida, ele chama o callback `onConfigChanged` para notificar outros componentes da alteração.
    *   Finalmente, ele atualiza a configuração atual e o hash da configuração.

## Papel e Importância

O módulo `pkg/hotreload` é um recurso valioso para melhorar a capacidade de gerenciamento e o tempo de atividade da aplicação `log_capturer_go`. Seus principais papéis são:

*   **Configuração Dinâmica:** Permite alterar a configuração da aplicação em tempo de execução sem a necessidade de uma reinicialização, o que é crucial para ambientes de alta disponibilidade.
*   **Automação:** Automatiza o processo de detecção e aplicação de alterações de configuração, reduzindo a necessidade de intervenção manual.
*   **Segurança:** Ao validar a nova configuração antes de aplicá-la e ao fornecer um mecanismo de backup, ajuda a evitar que configurações inválidas sejam carregadas.

## Configurações

A seção `hot_reload` do arquivo `config.yaml` é usada para configurar este módulo. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita o recarregamento a quente.
*   `watch_files`: Uma lista de arquivos de configuração adicionais a serem observados em busca de alterações.
*   `check_interval`: Com que frequência verificar se há alterações comparando os hashes dos arquivos.
*   `debounce_interval`: A quantidade de tempo a esperar após uma alteração no arquivo antes de acionar uma recarga.
*   `backup_on_reload`: Se deve fazer backup da configuração atual antes de recarregar.
*   `validate_on_reload`: Se deve validar a nova configuração antes de aplicá-la.

## Problemas e Melhorias

*   **Recarregamento Baseado em Callback:** A implementação atual depende de uma função de callback (`onConfigChanged`) para aplicar as alterações de configuração. Isso pode ser complexo de gerenciar, pois cada componente precisa registrar seu próprio callback. Uma abordagem mais centralizada e automatizada para aplicar as alterações de configuração poderia ser benéfica.
*   **Recargas Parciais:** A implementação atual recarrega toda a configuração quando qualquer parte dela muda. Uma implementação mais avançada poderia suportar recargas parciais, onde apenas as partes alteradas da configuração são aplicadas.
*   **Recargas Transacionais:** O processo de recarga não é transacional. Se ocorrer um erro ao aplicar a nova configuração, a aplicação pode ser deixada em um estado inconsistente. Uma abordagem transacional que possa reverter para a configuração anterior em caso de erro seria mais robusta.
