# Módulo: pkg/security

## Estrutura

*   `auth.go`: Este arquivo contém o `AuthManager`, que é responsável por lidar com a autenticação e autorização.
*   `auth_test.go`: Contém testes unitários para o `AuthManager`.
*   `input_validator.go`: Este arquivo contém o `InputValidator`, que fornece funções para validar e higienizar a entrada do usuário.

## Como funciona

O módulo `pkg/security` fornece um conjunto de ferramentas para proteger a aplicação.

#### `auth.go`

1.  **Autenticação (`Authenticate`):**
    *   O `AuthManager` suporta múltiplos métodos de autenticação, incluindo autenticação básica e autenticação baseada em token.
    *   **Autenticação Básica:** Valida o nome de usuário e a senha em uma lista de usuários configurados.
    *   **Autenticação por Token:** Valida um token do cabeçalho `Authorization` em uma lista de tokens configurados.
    *   Ele também implementa limitação de taxa para prevenir ataques de força bruta.

2.  **Autorização (`Authorize`):**
    *   A função `Authorize` verifica se um usuário autenticado tem permissão para realizar uma ação específica em um recurso.
    *   Ele usa um modelo de controle de acesso baseado em função (RBAC), onde os usuários são atribuídos a funções e as funções recebem permissões.

3.  **Middleware (`AuthMiddleware`):**
    *   A função `AuthMiddleware` fornece um middleware HTTP que pode ser usado para proteger os endpoints da API.
    *   Primeiro, ele autentica o usuário e, em seguida, autoriza a solicitação.

#### `input_validator.go`

1.  **Validação de Entrada (`ValidatePath`, `ValidateURL`, `ValidateString`):**
    *   O `InputValidator` fornece um conjunto de funções para validar diferentes tipos de entrada do usuário.
    *   `ValidatePath`: Valida caminhos de arquivo, verificando coisas como ataques de travessia de diretório e caracteres inválidos.
    *   `ValidateURL`: Valida URLs, verificando coisas como esquemas válidos e impedindo o uso de URLs privadas ou localhost em produção.
    *   `ValidateString`: Fornece validação genérica de strings, verificando coisas como comprimento máximo e caracteres de controle.

2.  **Higienização (`SanitizeForLogging`):**
    *   A função `SanitizeForLogging` higieniza os dados antes de serem registrados, redigindo segredos em potencial, como senhas e tokens.

## Papel e Importância

O módulo `pkg/security` é um componente crítico para proteger a aplicação `log_capturer_go` de ameaças de segurança. Seus principais papéis são:

*   **Autenticação:** Garante que apenas usuários autorizados possam acessar a API da aplicação.
*   **Autorização:** Garante que os usuários só possam realizar as ações que lhes são permitidas.
*   **Validação de Entrada:** Ajuda a prevenir uma variedade de vulnerabilidades de segurança, como travessia de diretório, cross-site scripting (XSS) e injeção de comando.
*   **Proteção de Dados:** As funções de higienização ajudam a evitar que informações confidenciais vazem para os logs.

## Configurações

O módulo `security` é configurado através da seção `security` do arquivo `config.yaml`. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita os recursos de segurança.
*   **Seção `authentication`:**
    *   `method`: O método de autenticação a ser usado (`basic`, `token`, etc.).
    *   `users`: Uma lista de usuários e seus hashes de senha.
    *   `tokens`: Um mapa de tokens de autenticação para nomes de usuário.
*   **Seção `authorization`:**
    *   `roles`: Um mapa de funções e suas permissões associadas.

## Problemas e Melhorias

*   **Hashing de Senha:** A função `verifyPassword` usa SHA256 para hashing de senha. Embora seja melhor que nada, um algoritmo de hashing mais seguro como bcrypt ou Argon2 deve ser usado.
*   **Suporte a JWT:** A autenticação JWT é atualmente um placeholder. Isso deve ser totalmente implementado para fornecer uma opção de autenticação mais moderna и flexível.
*   **Suporte a OAuth:** A configuração inclui uma seção para OAuth, mas a implementação está ausente. Esta seria uma adição valiosa para integração com provedores de autenticação de terceiros.
*   **Autorização Mais Granular:** O modelo de autorização atual é baseado em strings simples de recurso e ação. Um modelo mais avançado poderia suportar permissões mais granulares, como permissões no nível do objeto.
