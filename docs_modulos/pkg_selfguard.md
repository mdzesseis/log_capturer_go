# Módulo: pkg/selfguard

## Estrutura

*   `feedback_guard.go`: Este arquivo contém o `FeedbackGuard`, que é responsável por evitar que a aplicação entre em um loop de feedback ao monitorar seus próprios logs.

## Como funciona

O módulo `pkg/selfguard` fornece um mecanismo para identificar e filtrar as próprias entradas de log da aplicação.

1.  **Inicialização (`NewFeedbackGuard`):**
    *   Cria uma nova instância de `FeedbackGuard`.
    *   Inicializa uma lista de autoidentificadores, que são usados para identificar os próprios logs da aplicação. Eles podem ser configurados manualmente ou detectados automaticamente a partir de variáveis de ambiente.
    *   Compila um conjunto de expressões regulares para corresponder a nomes de contêineres, caminhos de arquivo e mensagens de log que devem ser excluídos.

2.  **Verificação de Entrada de Log (`CheckEntry`):**
    *   A função `CheckEntry` é chamada para cada entrada de log para determinar se é um autolog.
    *   Ela verifica o seguinte:
        *   Se o `source_id` da entrada de log corresponde a algum dos autoidentificadores.
        *   Se o nome do contêiner da entrada de log corresponde a algum dos autoidentificadores ou aos padrões de contêiner configurados.
        *   Se o caminho de origem da entrada de log corresponde a algum dos padrões de caminho configurados.
        *   Se o conteúdo da mensagem da entrada de log corresponde a algum dos padrões de mensagem configurados.
        *   Se a mensagem da entrada de log contém algum de uma lista de indicadores codificados que é um log da própria aplicação `log_capturer_go`.

3.  **Ação (`handleSelfLog`):**
    *   Se uma entrada de log for identificada como um autolog, a função `handleSelfLog` é chamada para determinar qual ação tomar.
    *   A ação pode ser uma das seguintes:
        *   `drop`: A entrada de log é descartada.
        *   `tag`: Uma tag especial é adicionada à entrada de log para indicar que é um autolog.
        *   `warn`: Um aviso é registrado, mas a entrada de log pode prosseguir.

## Papel e Importância

O módulo `pkg/selfguard` é um componente importante para evitar que a aplicação `log_capturer_go` entre em um loop de feedback. Isso pode acontecer se a aplicação for configurada para monitorar seus próprios arquivos de log ou contêineres. Se não for tratado adequadamente, isso pode levar a um loop infinito de geração e processamento de logs, que pode consumir rapidamente todos os recursos disponíveis.

## Configurações

A seção `self_guard` do arquivo `config.yaml` é usada para configurar este módulo. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita o protetor de feedback.
*   `self_container_name`: O nome do contêiner em que a aplicação está sendo executada.
*   `exclude_path_patterns`: Uma lista de expressões regulares para corresponder a caminhos de arquivo a serem excluídos.
*   `exclude_container_patterns`: Uma lista de expressões regulares para corresponder a nomes de contêineres a serem excluídos.
*   `self_log_action`: A ação a ser tomada quando um autolog é detectado (`drop`, `tag` ou `warn`).

## Problemas e Melhorias

*   **Indicadores Codificados:** A função `isLogCapturerLog` contém uma lista codificada de indicadores para identificar os próprios logs da aplicação. Isso poderia ser tornado mais configurável.
*   **Detecção Mais Sofisticada:** O mecanismo de detecção atual é baseado em correspondência simples de strings e expressões regulares. Uma implementação mais avançada poderia usar técnicas mais sofisticadas, como análise estatística ou aprendizado de máquina, para identificar autologs com mais precisão.
*   **Configuração Dinâmica:** Os autoidentificadores e os padrões de exclusão são atualmente carregados na inicialização. Seria benéfico permitir que eles fossem atualizados em tempo de execução sem a necessidade de uma reinicialização.
