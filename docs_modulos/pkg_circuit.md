# Módulo: pkg/circuit

## Estrutura

*   `breaker.go`: Este arquivo contém a struct `Breaker`, que implementa o padrão de Disjuntor (Circuit Breaker).

## Como funciona

O módulo `pkg/circuit` fornece uma implementação de Disjuntor para evitar chamadas repetidas a um serviço que está falhando.

1.  **Inicialização (`NewBreaker`):**
    *   Cria uma nova instância de `Breaker` com a configuração especificada.
    *   Define valores padrão para o limiar de falha, limiar de sucesso, tempo limite e outros parâmetros.

2.  **Estados:**
    *   O Disjuntor tem três estados:
        *   `Fechado`: O estado normal, onde as solicitações podem passar.
        *   `Aberto`: Quando o número de falhas excede o limiar, o circuito abre e todas as solicitações subsequentes são imediatamente rejeitadas sem serem executadas.
        *   `Meio-Aberto`: Após um período de tempo limite no estado `Aberto`, o circuito transita para `Meio-Aberto`. Neste estado, um número limitado de solicitações pode passar para testar se o serviço se recuperou.

3.  **Execução (`Execute`):**
    *   A função `Execute` envolve uma chamada de função com a lógica do Disjuntor.
    *   Primeiro, ela verifica o estado do circuito.
    *   Se o circuito estiver `Fechado`, ele executa a função e registra seu sucesso ou falha.
    *   Se o circuito estiver `Aberto`, ele rejeita a solicitação se o tempo limite ainda não tiver expirado. Se o tempo limite expirou, ele transita para `Meio-Aberto`.
    *   Se o circuito estiver `Meio-Aberto`, ele permite que um número limitado de solicitações passe. Se elas tiverem sucesso, o circuito volta para `Fechado`. Se falharem, ele volta para `Aberto`.

## Papel e Importância

O módulo `pkg/circuit` é um componente crucial para a construção de sistemas resilientes e tolerantes a falhas. Seus principais papéis são:

*   **Tolerância a Falhas:** Impede que a aplicação chame repetidamente um serviço que está falhando, o que poderia levar ao esgotamento de recursos e falhas em cascata.
*   **Falhas Rápidas:** Ao rejeitar imediatamente as solicitações quando o circuito está aberto, permite que o sistema falhe rapidamente, o que muitas vezes é preferível a esperar por um tempo limite.
*   **Recuperação Automática:** O estado `Meio-Aberto` permite que o Disjuntor detecte automaticamente quando um serviço com falha se recuperou, sem a necessidade de intervenção manual.

## Configurações

O módulo `circuit` é configurado através da struct `BreakerConfig`, que é tipicamente criada e passada para a função `NewBreaker` quando um coletor é inicializado. As principais configurações incluem:

*   `FailureThreshold`: O número de falhas consecutivas necessárias para abrir o circuito.
*   `SuccessThreshold`: O número de sucessos consecutivos necessários para fechar o circuito a partir do estado `Meio-Aberto`.
*   `Timeout`: A quantidade de tempo a esperar no estado `Aberto` antes de transitar para `Meio-Aberto`.
*   `HalfOpenMaxCalls`: O número máximo de solicitações a serem permitidas no estado `Meio-Aberto`.

## Problemas e Melhorias

*   **Disjuntores por Host:** A implementação atual cria um único Disjuntor para cada coletor. Em um ambiente distribuído com várias instâncias de um serviço downstream, pode ser benéfico ter um Disjuntor por host.
*   **Configuração Dinâmica:** Os parâmetros do Disjuntor são atualmente estáticos. Uma implementação mais avançada poderia ajustar dinamicamente os limiares e tempos limite com base na carga atual do sistema e nas taxas de erro.
*   **Transições de Estado Mais Sofisticadas:** A lógica de transição de estado poderia ser mais sofisticada. Por exemplo, poderia usar um aumento gradual do tráfego no estado `Meio-Aberto`.
