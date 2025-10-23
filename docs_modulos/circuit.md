# Módulo Circuit Breaker

## Estrutura e Operação

O módulo `circuit` implementa o padrão de projeto **Circuit Breaker**, que é uma técnica para construir sistemas mais resilientes. Ele envolve uma operação (como uma chamada de rede para um `sink`) e monitora suas falhas. Se o número de falhas exceder um limite, o circuit breaker "abre" e futuras chamadas falham imediatamente, sem tentar executar a operação.

### Principais Componentes da Estrutura:

- **`Breaker` Struct**: A estrutura principal que implementa a máquina de estados do Circuit Breaker.
- **`BreakerConfig` Struct**: Contém as configurações do breaker, como os limiares de falha e sucesso, e os timeouts.
- **Estados**: O `Breaker` opera em três estados:
    - **`Closed`**: O estado normal. As operações são executadas normalmente.
    - **`Open`**: Após um número configurável de falhas, o circuito abre. Todas as chamadas falham imediatamente por um período de `timeout`.
    - **`Half-Open`**: Após o `timeout`, o circuito entra neste estado e permite que um número limitado de chamadas de teste passe. Se elas forem bem-sucedidas, o circuito fecha. Se falharem, ele abre novamente.

### Fluxo de Operação:

1.  **Execução**: Uma operação é encapsulada pelo método `Execute()` do `Breaker`.
2.  **Monitoramento**: O `Breaker` conta o número de falhas consecutivas.
3.  **Abertura do Circuito**: Se o número de falhas atinge o `FailureThreshold`, o estado muda para `Open`. Todas as chamadas subsequentes a `Execute()` falham imediatamente.
4.  **Timeout e Half-Open**: Após o `Timeout` configurado, o estado muda para `Half-Open`.
5.  **Tentativa de Recuperação**: No estado `Half-Open`, um número limitado de chamadas (`HalfOpenMaxCalls`) é permitido. Se essas chamadas atingirem o `SuccessThreshold`, o circuito fecha (`Closed`). Se alguma falhar, ele volta para o estado `Open`.
6.  **Fechamento do Circuito**: No estado `Closed`, o contador de falhas é resetado após operações bem-sucedidas, e o sistema volta a operar normalmente.

## Papel e Importância

O módulo `circuit` é **fundamental para a resiliência e a estabilidade** do `log_capturer_go`, especialmente na comunicação com serviços externos (os `sinks`). Ele previne que a aplicação desperdice recursos tentando se comunicar com um serviço que está indisponível ou sobrecarregado.

Sua importância se traduz em:

- **Fail-Fast**: Permite que o sistema falhe rapidamente em vez de esperar por timeouts de rede, liberando recursos mais cedo.
- **Prevenção de Falhas em Cascata**: Impede que a falha de um único componente (um `sink`) se propague e derrube todo o sistema.
- **Recuperação Automática**: Dá tempo para que o serviço indisponível se recupere, e então tenta restabelecer a comunicação de forma controlada.

## Configurações Aplicáveis

As configurações para um `Breaker` são geralmente definidas dentro da configuração do componente que ele protege (como um `sink`):

- **`failure_threshold`**: O número de falhas consecutivas necessárias para abrir o circuito.
- **`success_threshold`**: O número de sucessos consecutivos no estado `Half-Open` para fechar o circuito.
- **`timeout`**: O tempo que o circuito permanecerá no estado `Open` antes de tentar se recuperar (transição para `Half-Open`).
- **`half_open_max_calls`**: O número máximo de chamadas permitidas no estado `Half-Open`.

## Problemas e Melhorias

### Problemas Potenciais:

- **Ajuste de Parâmetros**: Configurar os `thresholds` e `timeouts` de forma inadequada pode fazer com que o circuito abra muito facilmente (sendo muito sensível a falhas transitórias) ou que demore muito para abrir (não protegendo o sistema efetivamente).
- **Cenários de Rede Instável**: Em redes muito instáveis, o circuit breaker pode oscilar constantemente entre os estados `Open` e `Half-Open`, nunca se recuperando totalmente.

### Sugestões de Melhorias:

- **Backoff Exponencial no Timeout**: Em vez de um `timeout` fixo, o tempo no estado `Open` poderia aumentar exponencialmente a cada nova abertura, dando mais tempo para o serviço dependente se recuperar em caso de falhas recorrentes.
- **Métricas Detalhadas**: Expor mais métricas sobre o estado do circuit breaker (como o número de transições entre estados, o tempo gasto em cada estado) para o Prometheus, facilitando o monitoramento e o ajuste dos parâmetros.
- **Adaptação Dinâmica**: Tornar os `thresholds` adaptativos com base na taxa de sucesso/falha histórica, permitindo que o circuit breaker se ajuste automaticamente ao comportamento normal do serviço protegido.
- **Painel de Controle**: Adicionar um endpoint na API que permita visualizar o estado de todos os circuit breakers da aplicação e, se necessário, forçar manualmente a abertura ou o fechamento de um circuito para fins de teste ou manutenção.
