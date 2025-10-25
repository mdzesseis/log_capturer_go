# Módulo: pkg/task_manager

## Estrutura

*   `task_manager.go`: Este arquivo contém a struct `taskManager`, que implementa a interface `TaskManager` e é responsável por gerenciar o ciclo de vida das tarefas em segundo plano.

## Como funciona

O módulo `pkg/task_manager` fornece uma estrutura para executar e gerenciar tarefas em segundo plano de forma estruturada e confiável.

1.  **Inicialização (`New`):**
    *   Cria uma nova instância de `taskManager`.
    *   Define valores padrão para o intervalo de heartbeat, tempo limite da tarefa e intervalo de limpeza.
    *   Inicia uma goroutine em segundo plano (`cleanupLoop`) para limpar periodicamente as tarefas antigas e concluídas.

2.  **Iniciando Tarefas (`StartTask`):**
    *   O método `StartTask` é usado para iniciar uma nova tarefa em segundo plano.
    *   Ele recebe um ID de tarefa e uma função para executar como a tarefa.
    *   Ele cria uma nova struct `task` para rastrear o estado da tarefa, incluindo seu ID, estado, hora de início e último heartbeat.
    *   Em seguida, ele inicia a tarefa em uma nova goroutine.

3.  **Parando Tarefas (`StopTask`):**
    *   O método `StopTask` é usado para parar uma tarefa em execução.
    *   Ele cancela o contexto da tarefa, o que sinaliza para a tarefa parar.
    *   Em seguida, ele aguarda a conclusão da tarefa com um tempo limite.

4.  **Heartbeats (`Heartbeat`):**
    *   O método `Heartbeat` é usado pelas tarefas para relatar que ainda estão ativas e em execução.
    *   Se uma tarefa não enviar um heartbeat dentro do tempo limite da tarefa configurado, ela é considerada como esgotada e é parada.

5.  **Limpeza (`cleanupLoop`):**
    *   O `cleanupLoop` verifica periodicamente se há tarefas que expiraram ou foram concluídas por um longo tempo e as remove do gerenciador de tarefas.

## Papel e Importância

O módulo `pkg/task_manager` é um utilitário útil para gerenciar tarefas em segundo plano na aplicação `log_capturer_go`. Seus principais papéis são:

*   **Gerenciamento de Tarefas:** Fornece uma maneira centralizada de gerenciar o ciclo de vida das tarefas em segundo plano, incluindo iniciá-las, pará-las e monitorá-las.
*   **Confiabilidade:** O mecanismo de heartbeat e tempo limite ajuda a garantir que as tarefas não fiquem presas em um loop infinito ou se tornem irresponsivas.
*   **Observabilidade:** Os métodos `GetTaskStatus` e `GetAllTasks` fornecem uma maneira de monitorar o status das tarefas em execução, o que é útil para depuração e solução de problemas.

## Configurações

O módulo `task_manager` é configurado através da seção `task_manager` do arquivo `config.yaml`. As principais configurações incluem:

*   `heartbeat_interval`: Com que frequência as tarefas devem enviar um heartbeat.
*   `task_timeout`: A quantidade de tempo a esperar por um heartbeat antes de considerar uma tarefa como esgotada.
*   `cleanup_interval`: Com que frequência limpar tarefas antigas e concluídas.

## Problemas e Melhorias

*   **Dependências de Tarefas:** A implementação atual não suporta dependências entre tarefas. Uma implementação mais avançada poderia permitir a definição de dependências, para que uma tarefa só seja iniciada após a conclusão de suas dependências.
*   **Priorização de Tarefas:** A implementação atual não suporta a priorização de tarefas. Uma implementação mais avançada poderia permitir a atribuição de prioridades às tarefas, para que as tarefas mais importantes recebam mais recursos.
*   **Tarefas Persistentes:** A implementação atual gerencia apenas tarefas na memória. Uma implementação mais avançada poderia suportar tarefas persistentes que são reiniciadas automaticamente após uma falha ou reinicialização da aplicação.
