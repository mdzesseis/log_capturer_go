# Módulo: pkg/errors

## Estrutura

*   `errors.go`: Este arquivo define um tipo de erro padronizado para a aplicação, `AppError`, juntamente com níveis de severidade, códigos de erro e funções auxiliares para criar e trabalhar com esses erros.

## Como funciona

O módulo `pkg/errors` fornece uma maneira estruturada e consistente de lidar com erros em toda a aplicação.

1.  **Struct `AppError`:**
    *   Este é o núcleo do módulo. Ele define um tipo de erro personalizado que inclui:
        *   `Code`: Um código único para o erro (ex: `CONFIG_INVALID`, `NETWORK_TIMEOUT`).
        *   `Message`: Uma mensagem de erro legível por humanos.
        *   `Component`: O componente onde o erro ocorreu (ex: `config`, `dispatcher`).
        *   `Operation`: A operação que estava sendo realizada quando o erro ocorreu.
        *   `Cause`: O erro subjacente que causou este erro (para encapsulamento).
        *   `StackTrace`: Um rastreamento de pilha para ajudar na depuração.
        *   `Metadata`: Um mapa para adicionar contexto extra ao erro.
        *   `Timestamp`: Quando o erro ocorreu.
        *   `Severity`: A severidade do erro (`critical`, `high`, `medium`, `low`, `info`).

2.  **Criação de Erros:**
    *   A função `New` cria um novo `AppError`.
    *   Existem também funções auxiliares para criar erros com severidades específicas (`NewCritical`, `NewWithSeverity`) e para tipos de erro comuns (`ConfigError`, `ResourceError`, etc.).

3.  **Tratamento de Erros:**
    *   A struct `AppError` implementa a interface `error` padrão, então pode ser usada em qualquer lugar onde um `error` é esperado.
    *   O método `Wrap` permite encapsular um erro existente, o que é útil para adicionar contexto a erros de bibliotecas de terceiros.
    *   O método `WithMetadata` permite adicionar pares chave-valor arbitrários ao erro para um registro e depuração mais detalhados.

## Papel e Importância

O módulo `pkg/errors` é crucial para a observabilidade, depurabilidade e confiabilidade da aplicação. Seus principais papéis são:

*   **Padronização:** Fornece uma maneira padrão de representar e lidar com erros, o que torna o código mais consistente e fácil de entender.
*   **Contexto Rico:** Ao incluir informações como o componente, a operação e o rastreamento de pilha, torna muito mais fácil depurar erros quando eles ocorrem.
*   **Registro Estruturado:** O método `ToMap` permite registrar erros em um formato estruturado (ex: JSON), o que é ideal para consumo por ferramentas de análise de log.
*   **Alerta:** A severidade e os códigos de erro podem ser usados para criar regras de alerta mais inteligentes e direcionadas.

## Configurações

Este módulo não possui nenhuma configuração no arquivo `config.yaml`.

## Problemas e Melhorias

*   **Geração de Rastreamento de Pilha:** A geração de rastreamento de pilha é básica. Poderia ser melhorada para ser mais detalhada e para excluir quadros irrelevantes.
*   **Serialização de Erros:** O método `ToMap` é útil para registro estruturado, mas o módulo também poderia fornecer um método `ToJSON` para serializar erros para JSON para outros fins (ex: retornar erros de uma API).
*   **Internacionalização:** As mensagens de erro estão atualmente codificadas em inglês. Para uma aplicação que pode ser usada em diferentes regiões, seria benéfico suportar a internacionalização das mensagens de erro.
