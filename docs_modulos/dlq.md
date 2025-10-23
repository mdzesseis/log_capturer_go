# Módulo Dead Letter Queue (DLQ)

## Estrutura e Operação

O módulo `dlq` (Dead Letter Queue) serve como um repositório para logs que falharam em ser processados ou enviados aos `sinks` após múltiplas tentativas. Ele garante que nenhum log seja permanentemente perdido devido a falhas transitórias ou problemas de configuração.

### Principais Componentes da Estrutura:

- **`DeadLetterQueue`**: A estrutura principal que gerencia uma fila em memória e a persistência em disco das entradas da DLQ.
- **`DLQEntry`**: Representa uma entrada na DLQ. Contém o log original, a mensagem de erro, o `sink` que falhou, o número de tentativas e outros metadados para diagnóstico.
- **`AlertManager`**: Um componente interno que monitora o estado da DLQ e pode disparar alertas (via webhook ou email) se certos `thresholds` (limiares) forem atingidos.
- **Reprocessing Logic**: Lógica para tentar reprocessar os logs armazenados na DLQ em intervalos configuráveis.

### Fluxo de Operação:

1.  **Adição de Entradas**: Quando o `dispatcher` ou um `sink` falha em processar um log após todas as tentativas de `retry`, ele chama o método `AddEntry` da DLQ.
2.  **Enfileiramento e Persistência**: A entrada é primeiro colocada em uma fila em memória e, em seguida, escrita em um arquivo no disco para garantir a durabilidade.
3.  **Gerenciamento de Arquivos**: A DLQ gerencia seus próprios arquivos de log, com políticas de rotação por tamanho e limpeza por tempo de retenção.
4.  **Reprocessamento (Opcional)**: Se habilitado, uma rotina em background lê periodicamente os arquivos da DLQ e tenta reenviar os logs. Ele usa uma estratégia de `exponential backoff` para não sobrecarregar um `sink` que pode estar se recuperando.
5.  **Alertas**: O `AlertManager` monitora o tamanho da fila e a taxa de entrada na DLQ. Se os limiares forem excedidos, ele dispara alertas para notificar os administradores sobre um possível problema no pipeline de logs.

## Papel e Importância

O módulo `dlq` é a **última linha de defesa contra a perda de dados** no `log_capturer_go`. Ele é fundamental para a confiabilidade do sistema.

Sua importância se deve a:

- **Não Repúdio**: Garante que, mesmo que um log não possa ser entregue ao seu destino final, ele seja armazenado de forma segura para análise e reprocessamento manual ou automático.
- **Diagnóstico de Falhas**: As entradas na DLQ contêm informações valiosas sobre o motivo da falha, o que é essencial para diagnosticar problemas com os `sinks` ou com o formato dos logs.
- **Recuperação de Desastres**: Em caso de uma interrupção prolongada de um `sink`, a DLQ armazena os logs que, de outra forma, seriam perdidos. Uma vez que o `sink` se recupere, os logs podem ser reprocessados a partir da DLQ.

## Configurações Aplicáveis

As configurações para a `DeadLetterQueue` são definidas na seção `dispatcher` (para `dlq_enabled`) e em uma seção `dlq` dedicada no `config.yaml`:

- **`enabled`**: Habilita ou desabilita a DLQ.
- **`directory`**: O diretório onde os arquivos da DLQ serão armazenados.
- **`max_file_size_mb`**: Tamanho máximo de cada arquivo da DLQ antes da rotação.
- **`retention_days`**: Por quanto tempo os arquivos da DLQ devem ser mantidos.
- **`reprocessing.enabled`**: Habilita o reprocessamento automático de logs da DLQ.
- **`reprocessing.interval`**: Intervalo entre as tentativas de reprocessamento.
- **`alert_config.enabled`**: Habilita os alertas sobre o estado da DLQ.
- **`alert_config.entries_per_minute_threshold`**: Limiar de taxa de entrada para disparar um alerta.

## Problemas e Melhorias

### Problemas Potenciais:

- **Crescimento Ilimitado**: Se o problema com um `sink` persistir, a DLQ pode crescer indefinidamente e consumir todo o espaço em disco, tornando-se um ponto único de falha.
- **Performance de Reprocessamento**: O reprocessamento de um grande volume de logs da DLQ pode impactar a performance do processamento de logs em tempo real.
- **Análise Manual**: Analisar os arquivos da DLQ manualmente pode ser uma tarefa complexa, especialmente se eles estiverem em formato de texto não estruturado.

### Sugestões de Melhorias:

- **Interface de Visualização**: Criar uma pequena interface web ou CLI para visualizar, pesquisar e gerenciar as entradas na DLQ, facilitando o diagnóstico e o reprocessamento manual.
- **Reprocessamento Seletivo**: Permitir que o reprocessamento seja feito de forma seletiva, por exemplo, reenviar apenas logs de um `sink` específico ou de um determinado período.
- **Compressão de Arquivos da DLQ**: Adicionar compressão automática aos arquivos da DLQ para economizar espaço em disco.
- **Estratégias de Descarte**: Implementar estratégias de descarte mais inteligentes quando a DLQ atinge sua capacidade máxima, como descartar logs de menor prioridade primeiro.
