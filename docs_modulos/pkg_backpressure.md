# Módulo: pkg/backpressure

## Estrutura

*   `manager.go`: Este arquivo contém a struct `Manager`, que é responsável por gerenciar a contrapressão (backpressure) do sistema.

## Como funciona

O módulo `pkg/backpressure` fornece um mecanismo para lidar graciosamente com altas cargas e evitar que o sistema seja sobrecarregado.

1.  **Inicialização (`NewManager`):**
    *   Cria uma nova instância do `Manager` com a configuração especificada.
    *   Define valores padrão para limiares, intervalos de verificação e fatores de redução, se não forem fornecidos.

2.  **Atualização de Métricas (`UpdateMetrics`):**
    *   Esta função é chamada periodicamente para atualizar o gerenciador de contrapressão com as métricas mais recentes do sistema, incluindo:
        *   `QueueUtilization`: A utilização da fila do dispatcher.
        *   `MemoryUtilization`: O uso de memória da aplicação.
        *   `CPUUtilization`: O uso de CPU da aplicação.
        *   `IOUtilization`: A utilização de I/O da aplicação.
        *   `ErrorRate`: A taxa de erros na aplicação.

3.  **Avaliação de Nível (`evaluateLevel`):**
    *   Com base nas métricas atualizadas, esta função calcula uma pontuação de carga geral do sistema.
    *   Em seguida, determina o nível de contrapressão apropriado (`LevelNone`, `LevelLow`, `LevelMedium`, `LevelHigh` ou `LevelCritical`) com base nos limiares configurados.
    *   Se o novo nível for diferente do nível atual, ele chama `changeLevel` para aplicar as novas configurações de contrapressão.

4.  **Aplicação de Contrapressão (`changeLevel`):**
    *   Quando o nível de contrapressão muda, esta função atualiza o fator de redução atual.
    *   O fator de redução é um valor entre 0.0 e 1.0 que determina o quanto a aplicação deve desacelerar.
    *   Ele também aciona uma função de callback (`onLevelChange`) para notificar outros componentes sobre a mudança no nível de contrapressão.

5.  **Throttling e Rejeição:**
    *   Outros componentes da aplicação могут usar os métodos `ShouldThrottle` e `ShouldReject` para determinar se devem desacelerar ou rejeitar novas solicitações.
    *   `ShouldThrottle` retorna `true` se o nível de contrapressão for `LevelMedium` ou superior.
    *   `ShouldReject` retorna `true` se o nível de contrapressão for `LevelCritical`.

## Papel e Importância

O módulo `pkg/backpressure` é um componente crítico para garantir a estabilidade e a confiabilidade da aplicação `log_capturer_go`, especialmente sob alta carga. Seus principais papéis são:

*   **Proteção contra Sobrecarga:** Impede que a aplicação seja sobrecarregada por logs recebidos, o que poderia levar a falhas ou perda de dados.
*   **Degradação Graciosa:** Ao aumentar gradualmente o nível de contrapressão, permite que a aplicação degrade seu desempenho graciosamente em vez de falhar completamente.
*   **Estabilidade do Sistema:** Ajuda a manter a estabilidade geral do sistema, evitando o esgotamento de recursos.

## Configurações

O módulo `backpressure` é configurado através da seção `dispatcher.backpressure_config` do arquivo `config.yaml`. As principais configurações incluem:

*   `low_threshold`, `medium_threshold`, `high_threshold`, `critical_threshold`: Os limiares para os diferentes níveis de contrapressão.
*   `check_interval`: Com que frequência verificar as métricas do sistema e ajustar o nível de contrapressão.
*   `low_reduction`, `medium_reduction`, `high_reduction`, `critical_reduction`: Os fatores de redução para cada nível de contrapressão.

## Problemas e Melhorias

*   **Coleta de Métricas:** A implementação atual para coletar a utilização de CPU e I/O é uma estimativa simplificada. Um método mais preciso para coletar essas métricas melhoraria a eficácia do mecanismo de contrapressão.
*   **Contrapressão por Componente:** O mecanismo de contrapressão atual é global. Uma implementação mais avançada poderia aplicar contrapressão por componente ou por coletor.
*   **Limiares Dinâmicos:** Os limiares de contrapressão são atualmente estáticos. Uma implementação mais avançada poderia ajustar dinamicamente os limiares com base em dados históricos e tendências.
