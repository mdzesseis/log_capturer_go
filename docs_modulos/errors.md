# Módulo Errors

## Estrutura e Operação

O módulo `errors` fornece um sistema padronizado e estruturado para o gerenciamento de erros em toda a aplicação `log_capturer_go`. Ele substitui o uso de erros genéricos do Go (`error`) por uma estrutura mais rica e informativa, facilitando o diagnóstico e o monitoramento de problemas.

### Principais Componentes da Estrutura:

- **`AppError` Struct**: A estrutura central do módulo. Um `AppError` encapsula informações detalhadas sobre um erro, incluindo:
    - **`Code`**: Um código de erro padronizado (ex: `CONFIG_INVALID`, `NETWORK_TIMEOUT`).
    - **`Message`**: Uma mensagem descritiva do erro.
    - **`Component`** e **`Operation`**: Onde o erro ocorreu.
    - **`Cause`**: O erro original que foi encapsulado (wrapped).
    - **`StackTrace`**: Um stack trace para ajudar na depuração.
    - **`Severity`**: O nível de severidade do erro (`Critical`, `High`, `Medium`, `Low`).
- **Constantes de Código e Severidade**: O módulo define um conjunto de constantes para os códigos de erro e os níveis de severidade, garantindo a consistência no tratamento de erros.
- **Funções Construtoras**: Funções como `New()`, `NewCritical()`, e `WrapError()` facilitam a criação de `AppError` de forma padronizada.

### Fluxo de Operação:

1.  **Criação do Erro**: Quando um erro ocorre em algum componente, em vez de retornar um erro padrão, o componente cria um `AppError` usando uma das funções construtoras (ex: `errors.NetworkError(...)`).
2.  **Encapsulamento (Wrapping)**: Se a função estiver tratando um erro de uma biblioteca de terceiros, ela pode "encapsular" esse erro original dentro de um `AppError` usando o método `Wrap()`. Isso preserva a informação do erro original.
3.  **Propagação**: O `AppError` é retornado pela cadeia de chamadas.
4.  **Tratamento Estruturado**: O componente que finalmente trata o erro pode inspecionar os campos do `AppError` (como `Code` e `Severity`) para tomar decisões mais inteligentes, como:
    - Logar o erro com todos os seus metadados.
    - Decidir se a operação deve ser tentada novamente (`IsRecoverable()`).
    - Retornar um código de status HTTP apropriado em um endpoint de API.

## Papel e Importância

O módulo `errors` é **fundamental para a robustez e a manutenibilidade** da aplicação. Erros bem estruturados são a base para um bom monitoramento, alertas eficazes e uma depuração mais rápida.

Sua importância se manifesta em:

- **Diagnóstico Rápido**: Ao fornecer um contexto rico (componente, operação, stack trace, causa), os `AppError` permitem que os desenvolvedores identifiquem a causa raiz de um problema muito mais rapidamente.
- **Monitoramento e Alertas**: Os códigos de erro padronizados e os níveis de severidade podem ser facilmente transformados em métricas no Prometheus. Isso permite a criação de alertas específicos, como "alertar se o número de erros `NETWORK_TIMEOUT` no componente `loki_sink` aumentar".
- **Consistência**: Garante que todos os erros na aplicação sigam o mesmo formato, tornando o código de tratamento de erros mais limpo e previsível.
- **Tratamento Inteligente**: Permite que a aplicação tome decisões com base no tipo e na severidade do erro, como decidir se uma falha é recuperável ou se requer uma intervenção imediata.

## Configurações Aplicáveis

O módulo `errors` não possui configurações externas diretas no `config.yaml`. Ele é um módulo de utilidade interna usado por outros componentes. As "configurações" são, na verdade, os códigos de erro e as severidades definidas como constantes no próprio código.

## Problemas e Melhorias

### Problemas Potenciais:

- **Proliferação de Códigos**: Se não for bem gerenciado, o número de códigos de erro pode crescer muito, tornando-se difícil de manter.
- **Uso Inconsistente**: Os desenvolvedores podem esquecer de usar o `AppError` e recorrer a erros padrão, perdendo os benefícios da estruturação.

### Sugestões de Melhorias:

- **Documentação de Códigos de Erro**: Manter uma documentação centralizada (talvez gerada automaticamente a partir do código) que explique o significado de cada `Code` de erro e as possíveis causas.
- **Internacionalização (i18n)**: Para aplicações que precisam de mensagens de erro em múltiplos idiomas, a estrutura `AppError` poderia ser estendida para suportar a internacionalização das mensagens.
- **Integração com Ferramentas de Tracing**: Integrar os `AppError` com o sistema de tracing (como OpenTelemetry) para que os erros sejam automaticamente associados a um `trace` ou `span` específico, enriquecendo ainda mais o contexto de depuração.
- **Análise de Frequência de Erros**: Criar uma ferramenta ou um dashboard que analise a frequência de cada código de erro, ajudando a identificar os problemas mais comuns na aplicação.
