# Módulo Rate Limit

## Estrutura e Operação

O módulo `ratelimit` (implementado como `AdaptiveRateLimiter`) fornece um mecanismo de limitação de taxa (rate limiting) que se adapta dinamicamente às condições do sistema. Ele utiliza uma variação do algoritmo **Token Bucket**, mas com a capacidade de ajustar a taxa de preenchimento do balde (RPS - Requests Per Second) com base na latência observada.

### Principais Componentes da Estrutura:

- **`AdaptiveRateLimiter`**: A estrutura principal que implementa a lógica do token bucket e o algoritmo de adaptação.
- **`Config`**: Contém as configurações do limiter, como os limites de RPS (inicial, mínimo, máximo), o alvo de latência e os fatores de adaptação.
- **`LatencyWindow`**: Uma estrutura de dados que armazena uma janela deslizante das últimas latências de operação registradas, permitindo o cálculo de uma média móvel.

### Fluxo de Operação:

1.  **Verificação (`Allow`)**: Antes de executar uma operação que deve ser limitada (como o `dispatcher` receber um novo log), o método `Allow()` é chamado.
2.  **Token Bucket**: O `Allow()` implementa a lógica do token bucket:
    - Calcula quantos "tokens" foram adicionados ao balde desde a última verificação, com base na taxa atual (`currentRPS`).
    - Se houver pelo menos um token no balde, a requisição é permitida, e um token é consumido.
    - Se não houver tokens, a requisição é bloqueada (rejeitada).
3.  **Registro de Latência**: O componente que utiliza o rate limiter (ex: um `sink`) mede a duração de suas operações e informa ao `AdaptiveRateLimiter` através do método `RecordLatency()`.
4.  **Adaptação**: Em intervalos regulares, uma goroutine (`adaptationLoop`) analisa a latência média registrada na `LatencyWindow`:
    - Se a latência média está **acima** do `LatencyTargetMS`, o `currentRPS` é **reduzido**, tornando o rate limit mais restritivo.
    - Se a latência média está **abaixo** do alvo, o `currentRPS` é **aumentado**, tornando o rate limit mais permissivo, até atingir o `MaxRPS`.

## Papel e Importância

O `ratelimit` é um mecanismo de proteção proativo. Diferente do `backpressure`, que reage a um sistema já sobrecarregado (filas cheias), o rate limiting tenta **prevenir a sobrecarga** em primeiro lugar, controlando a taxa de entrada de requisições.

Sua importância está em:

- **Proteção de Sinks**: Evita que o `log_capturer_go` envie logs para os `sinks` (Loki, Elasticsearch, etc.) a uma taxa maior do que eles conseguem suportar, prevenindo que eles fiquem sobrecarregados ou comecem a rejeitar requisições.
- **Auto-Ajuste**: A natureza adaptativa do limiter é sua principal vantagem. Ele "aprende" a capacidade do sistema de destino com base na latência e se ajusta a ela, encontrando a taxa de envio ótima sem a necessidade de configuração manual constante.
- **Previsibilidade**: Ajuda a suavizar picos de tráfego, criando um fluxo de saída mais previsível e estável.

## Configurações Aplicáveis

As configurações para o `AdaptiveRateLimiter` são definidas na seção `rate_limit` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita o rate limiting.
- **`initial_rps`**: A taxa inicial de requisições por segundo com a qual o sistema começa.
- **`min_rps`** e **`max_rps`**: Os limites mínimo e máximo para a taxa de requisições.
- **`latency_target_ms`**: A latência de operação (em milissegundos) que o sistema tentará manter. É o principal parâmetro para o algoritmo de adaptação.
- **`latency_tolerance`**: Uma porcentagem de tolerância acima do alvo de latência antes que o sistema comece a reduzir a taxa.
- **`adaptation_interval`**: A frequência com que o algoritmo de adaptação é executado.

## Problemas e Melhorias

### Problemas Potenciais:

- **Medição de Latência**: O sistema depende de medições de latência precisas. Se a latência não for registrada corretamente, o algoritmo de adaptação não funcionará como esperado.
- **Ajuste do Alvo de Latência**: Definir um `latency_target_ms` irrealista (muito baixo ou muito alto) pode fazer com que o rate limiter seja excessivamente restritivo ou permissivo demais.
- **Cenários de Burst**: A configuração de `burst` (o tamanho do "balde" de tokens) precisa ser bem ajustada para permitir picos de tráfego legítimos sem bloquear requisições desnecessariamente.

### Sugestões de Melhorias:

- **Algoritmos de Adaptação Alternativos**: Explorar outros algoritmos de controle de congestionamento, como o **Vegas** ou o **BBR**, que podem oferecer uma adaptação mais estável e eficiente do que o método atual baseado em limiares.
- **Rate Limiting Distribuído**: Em um cenário onde múltiplas instâncias do `log_capturer_go` enviam dados para o mesmo `sink`, um sistema de rate limiting distribuído (usando, por exemplo, Redis ou um serviço similar) seria necessário para coordenar a taxa de envio total.
- **Limitação por Bytes**: Além de limitar por requisições por segundo, adicionar a capacidade de limitar por bytes por segundo, o que pode ser mais relevante para alguns `sinks`.
- **Múltiplos Limiters**: Permitir a configuração de diferentes `rate limiters` para diferentes `sinks` ou tipos de log, já que cada destino pode ter uma capacidade diferente.
