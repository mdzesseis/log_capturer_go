# Módulo: pkg/validation

## Estrutura

*   `timestamp_validator.go`: Este arquivo contém o `TimestampValidator`, que é responsável por validar e ajustar os timestamps das entradas de log.
*   `timestamp_validator_test.go`: Contém testes unitários para o `TimestampValidator`.

## Como funciona

O módulo `pkg/validation` fornece um mecanismo para garantir que os timestamps das entradas de log sejam válidos e estejam dentro de um intervalo aceitável.

1.  **Inicialização (`NewTimestampValidator`):**
    *   Cria uma nova instância de `TimestampValidator`.
    *   Define valores padrão para a idade máxima permitida de timestamps no passado e no futuro, o fuso horário padrão e os formatos de timestamp aceitos.

2.  **Validação de Timestamp (`ValidateTimestamp`):**
    *   A função `ValidateTimestamp` é chamada para cada entrada de log para validar seu timestamp.
    *   Ela verifica se o timestamp está muito no futuro ou muito no passado, com base nos limiares configurados.
    *   Se o timestamp for inválido, ele chama `handleInvalidTimestamp` para determinar qual ação tomar.

3.  **Tratamento de Timestamps Inválidos (`handleInvalidTimestamp`):**
    *   A função `handleInvalidTimestamp` pode ser configurada para tomar uma das seguintes ações:
        *   `clamp`: O timestamp é ajustado para ficar dentro do intervalo aceitável.
        *   `reject`: A entrada de log é rejeitada.
        *   `warn`: Um aviso é registrado, mas a entrada de log pode prosseguir com o timestamp original.

4.  **Análise de Timestamp (`ParseTimestamp`):**
    *   A função `ParseTimestamp` pode ser usada para analisar uma string de timestamp em um objeto `time.Time`.
    *   Ela tenta analisar a string usando uma lista de formatos configurados.

## Papel e Importância

O módulo `pkg/validation` é um componente importante para garantir a qualidade e a consistência dos dados de log. Seus principais papéis são:

*   **Qualidade dos Dados:** Garante que todas as entradas de log tenham um timestamp válido e razoável.
*   **Consistência:** Ajuda a garantir que todos os timestamps estejam em um formato e fuso horário consistentes.
*   **Prevenção de Erros:** Pode prevenir erros em sistemas downstream que podem ser causados por timestamps inválidos ou fora do intervalo.

## Configurações

A seção `timestamp_validation` do arquivo `config.yaml` é usada para configurar este módulo. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita a validação de timestamp.
*   `max_past_age_seconds`: A idade máxima permitida de timestamps no passado.
*   `max_future_age_seconds`: A idade máxima permitida de timestamps no futuro.
*   `invalid_action`: A ação a ser tomada quando um timestamp inválido é detectado (`clamp`, `reject` ou `warn`).
*   `accepted_formats`: Uma lista de formatos de timestamp aceitos para análise.

## Problemas e Melhorias

*   **Tratamento de Fuso Horário:** O tratamento de fuso horário poderia ser tornado mais robusto. Por exemplo, ele poderia ser configurado para detectar automaticamente o fuso horário da entrada de log ou para usar um fuso horário diferente para cada fonte de log.
*   **Regras de Validação Mais Flexíveis:** As regras de validação atuais são baseadas em limiares de idade simples. Uma implementação mais avançada poderia suportar regras mais flexíveis, como permitir uma certa quantidade de desvio de relógio entre diferentes sistemas.
*   **Integração com Pipelines de Processamento:** A validação de timestamp poderia ser mais integrada com os pipelines de processamento, para que possa ser aplicada como uma etapa em um pipeline.
