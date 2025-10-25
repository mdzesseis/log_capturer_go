# Módulo: pkg/workerpool

## Estrutura

*   `worker_pool.go`: Este arquivo contém o componente `WorkerPool`, que é responsável por gerenciar um pool de goroutines de trabalho para executar tarefas simultaneamente.

## Como funciona

O módulo `pkg/workerpool` fornece um mecanismo para gerenciar um pool de goroutines de trabalho para processar tarefas em paralelo.

1.  **Inicialização (`NewWorkerPool`):**
    *   Cria uma nova instância de `WorkerPool`.
    *   Define valores padrão para o número máximo de trabalhadores, o tamanho da fila e outros parâmetros de configuração.
    *   Cria um pool de goroutines de trabalho.

2.  **Submissão de Tarefas (`SubmitTask`, `SubmitTaskWithTimeout`):**
    *   O método `SubmitTask` é usado para submeter uma nova tarefa ao pool de trabalhadores.
    *   A tarefa é adicionada a uma fila de tarefas, onde aguarda para ser pega por um trabalhador.
    *   O método `SubmitTaskWithTimeout` é semelhante, mas permite especificar um tempo limite para submeter a tarefa.

3.  **Execução de Tarefas (`dispatcher`, `executeTask`):**
    *   Uma goroutine `dispatcher` lê as tarefas da fila de tarefas e as atribui aos trabalhadores disponíveis.
    *   Cada trabalhador tem seu próprio canal de tarefas e executa as tarefas uma de cada vez.
    *   A função `executeTask` executa a função `Execute` da tarefa e registra o resultado (sucesso ou falha).

4.  **Métricas e Estatísticas:**
    *   O pool de trabalhadores coleta uma variedade de métricas, como o número de trabalhadores ativos, o número de tarefas na fila e o número de tarefas concluídas e com falha.
    *   Uma goroutine `metricsCollector` registra periodicamente essas métricas.

## Papel e Importância

O módulo `pkg/workerpool` é um utilitário útil para gerenciar tarefas simultâneas na aplicação `log_capturer_go`. Seus principais papéis são:

*   **Gerenciamento de Concorrência:** Fornece uma maneira simples e eficiente de gerenciar um pool de goroutines de trabalho, que pode ser usado para processar tarefas em paralelo.
*   **Gerenciamento de Recursos:** Ajuda a limitar o número de tarefas simultâneas, o que pode impedir que a aplicação consuma muitos recursos.
*   **Observabilidade:** As métricas e estatísticas coletadas pelo pool de trabalhadores fornecem insights sobre o desempenho e a saúde do sistema de processamento de tarefas.

## Configurações

O módulo `worker_pool` é configurado através da seção `worker_pool` do arquivo `config.yaml`. As principais configurações incluem:

*   `max_workers`: O número máximo de goroutines de trabalho no pool.
*   `queue_size`: O tamanho da fila de tarefas.
*   `worker_timeout`: A quantidade de tempo a esperar pela conclusão de uma tarefa antes de considerá-la como esgotada.
*   `idle_timeout`: A quantidade de tempo que um trabalhador pode ficar ocioso antes de ser parado.

## Problemas e Melhorias

*   **Priorização de Tarefas:** A implementação atual usa uma fila FIFO (primeiro a entrar, primeiro a sair) simples para as tarefas. Uma implementação mais avançada poderia suportar a priorização de tarefas, para que as tarefas mais importantes sejam processadas primeiro.
*   **Tamanho Dinâmico do Pool:** O tamanho do pool de trabalhadores é fixo na inicialização. Uma implementação mais avançada poderia ajustar dinamicamente o tamanho do pool com base na carga de trabalho atual.
*   **Tratamento de Erros:** O tratamento de erros poderia ser melhorado. Por exemplo, o pool de trabalhadores poderia ser configurado para tentar novamente automaticamente as tarefas com falha ou para enviá-las para uma fila de mensagens mortas.
