# Módulo Backpressure

## Estrutura e Operação

O módulo `backpressure` implementa um sistema de gerenciamento de contrapressão adaptativo, projetado para proteger o `log_capturer_go` e seus sistemas de destino contra sobrecarga. Ele monitora diversas métricas do sistema e, com base nelas, ajusta o comportamento da aplicação para reduzir a carga.

### Principais Componentes da Estrutura:

- **`Manager` Struct**: A estrutura central que monitora as métricas do sistema, calcula um "score" de carga e determina o nível de backpressure a ser aplicado.
- **`Level` Enum**: Define os diferentes níveis de backpressure: `None`, `Low`, `Medium`, `High`, e `Critical`.
- **`Config` Struct**: Contém as configurações do gerenciador, como os `thresholds` (limiares) para cada nível, os intervalos de verificação e os fatores de redução de carga.
- **`Metrics` Struct**: Representa as métricas que são usadas para calcular a carga do sistema, incluindo utilização de fila, memória, CPU, I/O e taxa de erro.

### Fluxo de Operação:

1.  **Coleta de Métricas**: O `Manager` recebe atualizações de métricas de diferentes partes do sistema (como o `dispatcher`).
2.  **Cálculo do Score de Carga**: Ele calcula um score de carga geral, que é uma média ponderada das diferentes métricas (utilização de fila, CPU, memória, etc.).
3.  **Determinação do Nível**: Com base no score de carga, o `Manager` determina o nível de backpressure apropriado (`None`, `Low`, `Medium`, `High`, `Critical`).
4.  **Aplicação de Ações**: Dependendo do nível, o `Manager` pode tomar diferentes ações:
    - **`ShouldThrottle()`**: Indica que a aplicação deve começar a reduzir a taxa de ingestão de novos logs.
    - **`ShouldReject()`**: Em níveis críticos, indica que a aplicação deve parar de aceitar novos logs temporariamente.
    - **`ShouldDegrade()`**: Sugere que funcionalidades não essenciais (como processamento complexo ou deduplicação) devem ser desativadas para economizar recursos.
5.  **Adaptação**: O `Manager` reavalia periodicamente o nível de backpressure, aumentando ou diminuindo a contrapressão conforme a carga do sistema muda.

## Papel e Importância

O módulo `backpressure` é **essencial para a estabilidade e a resiliência** do `log_capturer_go` em ambientes de alta carga. Ele atua como um mecanismo de segurança que previne falhas em cascata quando o volume de logs excede a capacidade de processamento ou de envio.

Sua importância se destaca em:

- **Prevenção de Sobrecarga**: Evita que a aplicação consuma todos os recursos do sistema (CPU, memória) ou que sobrecarregue os `sinks`.
- **Estabilidade**: Mantém a aplicação funcionando de forma estável, mesmo sob picos de carga, ao invés de travar ou falhar.
- **Degradação Graciosa**: Permite que a aplicação se degrade de forma controlada, desativando funcionalidades menos críticas para manter a coleta e o envio de logs essenciais em funcionamento.

## Configurações Aplicáveis

As configurações do `backpressure` são definidas na seção `backpressure` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita o gerenciamento de backpressure.
- **`low_threshold`, `medium_threshold`, `high_threshold`, `critical_threshold`**: Limiares (de 0.0 a 1.0) para ativar cada nível de backpressure.
- **`check_interval`**: Com que frequência o `Manager` deve verificar as métricas e reavaliar o nível.
- **`stabilize_time`**: Período de tempo que o sistema deve permanecer em um novo nível antes de permitir outra mudança, para evitar oscilações rápidas.
- **`cooldown_time`**: Tempo de espera após uma mudança de nível antes que outra possa ocorrer.

## Problemas e Melhorias

### Problemas Potenciais:

- **Ajuste de Thresholds**: Configurar os `thresholds` e os pesos das métricas de forma inadequada pode fazer com que o sistema de backpressure seja muito agressivo (rejeitando logs desnecessariamente) ou muito passivo (não agindo quando deveria).
- **Métricas Imprecisas**: O cálculo do score de carga depende da precisão das métricas coletadas. Se as métricas de CPU ou memória não forem precisas, a resposta do sistema pode ser inadequada.
- **Oscilação**: O sistema pode oscilar rapidamente entre diferentes níveis de backpressure se os tempos de `stabilize` e `cooldown` não estiverem bem ajustados.

### Sugestões de Melhorias:

- **Auto-Tuning de Thresholds**: Implementar um mecanismo de aprendizado que ajuste automaticamente os `thresholds` com base no comportamento histórico da aplicação.
- **Métricas de CPU e I/O Mais Precisas**: Integrar com bibliotecas mais avançadas (como `gopsutil`) para obter medições mais precisas do uso de CPU e I/O, em vez de depender de estimativas.
- **Estratégias de Throttling Mais Granulares**: Permitir a configuração de estratégias de throttling mais sofisticadas, como a rejeição de logs com base em sua prioridade ou origem, em vez de uma rejeição probabilística.
- **Integração com Circuit Breaker**: Integrar o `backpressure` com o módulo `circuitbreaker` para que um nível crítico de backpressure possa abrir o circuit breaker de um sink específico que está causando a lentidão.
