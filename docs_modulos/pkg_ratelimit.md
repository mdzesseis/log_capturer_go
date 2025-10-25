# Módulo: pkg/ratelimit

## Estrutura

*   `adaptive_limiter.go`: Este arquivo contém o `AdaptiveRateLimiter`, que implementa um algoritmo de limitação de taxa adaptativo com base na latência.

## Como funciona

O módulo `pkg/ratelimit` fornece um mecanismo para controlar a taxa de solicitações de saída para evitar a sobrecarga dos sistemas downstream.

1.  **Inicialização (`NewAdaptiveRateLimiter`):**
    *   Cria uma nova instância de `AdaptiveRateLimiter`.
    *   Define valores padrão para o RPS (requisições por segundo) inicial, tamanho do burst e outros parâmetros de configuração.
    *   Inicializa uma `LatencyWindow` para rastrear as latências recentes das solicitações de saída.
    *   Inicia uma goroutine em segundo plano (`adaptationLoop`) para ajustar periodicamente o limite de taxa.

2.  **Limitação de Taxa (`Allow`, `AllowN`, `AllowBytes`):**
    *   O método `Allow` verifica se uma única solicitação pode prosseguir.
    *   Ele usa um algoritmo de balde de tokens, onde os tokens são adicionados ao balde a uma taxa determinada pelo `currentRPS`.
    *   Se houver tokens suficientes no balde, a solicitação é permitida e o número de tokens é decrementado. Caso contrário, a solicitação é rejeitada.
    *   Os métodos `AllowN` e `AllowBytes` são semelhantes, mas permitem verificar se um determinado número de solicitações ou um determinado número de bytes são permitidos.

3.  **Adaptação (`adaptationLoop` e `performAdaptation`):**
    *   O `adaptationLoop` chama periodicamente `performAdaptation` para ajustar o limite de taxa.
    *   A função `performAdaptation` calcula a latência média das solicitações recentes.
    *   **Se a latência média for muito alta:** Ele reduz o `currentRPS` para diminuir a taxa de solicitações.
    *   **Se a latência média for baixa:** Ele aumenta o `currentRPS` para permitir um throughput maior.
    *   Isso permite que o limitador de taxa encontre automaticamente a taxa ideal para as condições atuais da rede e a capacidade do sistema downstream.

## Papel e Importância

O módulo `pkg/ratelimit` é um componente importante para garantir a estabilidade e a confiabilidade da aplicação `log_capturer_go` e dos sistemas com os quais ela interage. Seus principais papéis são:

*   **Proteção contra Sobrecarga:** Impede que a aplicação sobrecarregue os sistemas downstream com muitas solicitações, o que poderia torná-los lentos ou irresponsivos.
*   **Desempenho Adaptativo:** Ajusta automaticamente a taxa de solicitações para corresponder à capacidade do sistema downstream, o que ajuda a maximizar o throughput sem causar sobrecarga.
*   **Justiça:** Garante que todos os componentes da aplicação tenham uma chance justa de enviar solicitações, mesmo quando o sistema está sob alta carga.

## Configurações

O módulo `rate_limit` é configurado através da seção `dispatcher.rate_limit_config` do arquivo `config.yaml`. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita a limitação de taxa.
*   `initial_rps`: O limite inicial de requisições por segundo.
*   `min_rps`, `max_rps`: O RPS mínimo e máximo permitidos.
*   `latency_target_ms`: A latência alvo que o limitador de taxa tentará alcançar.
*   `adaptation_interval`: Com que frequência ajustar o limite de taxa.

## Problemas e Melhorias

*   **Medição de Latência:** O limitador de taxa depende de outros componentes para registrar a latência das solicitações de saída. Isso poderia ser tornado mais robusto integrando a medição de latência diretamente nas implementações do coletor.
*   **Lógica de Adaptação Mais Sofisticada:** A lógica de adaptação atual é baseada em heurísticas simples. Algoritmos mais avançados, como um controlador PID ou um modelo de aprendizado de máquina, poderiam ser usados para um controle mais preciso sobre o limite de taxa.
*   **Limitação de Taxa por Host:** A implementação atual aplica um único limite de taxa a todas as solicitações de saída. Em um ambiente distribuído, pode ser benéfico ter um limite de taxa por host ou por serviço.
