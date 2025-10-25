# Módulo: pkg/degradation

## Estrutura

*   `manager.go`: Este arquivo contém a struct `Manager`, que é responsável por degradar graciosamente a funcionalidade da aplicação sob alta carga.

## Como funciona

O módulo `pkg/degradation` funciona em conjunto com o módulo `pkg/backpressure` para desabilitar seletivamente recursos não essenciais quando o sistema está sob estresse.

1.  **Inicialização (`NewManager`):**
    *   Cria uma nova instância do `Manager`.
    *   Define quais recursos devem ser degradados em cada nível de contrapressão (`LevelLow`, `LevelMedium`, `LevelHigh`, `LevelCritical`).
    *   Inicializa o estado de todos os recursos como `enabled`.

2.  **Atualização de Nível (`UpdateLevel`):**
    *   Esta função é chamada pelo `BackpressureManager` sempre que o nível de contrapressão muda.
    *   Ela determina quais recursos degradar com base no novo nível de contrapressão e nas políticas de degradação configuradas.
    *   Se o nível de contrapressão diminuir, ele agenda uma operação de restauração para reativar os recursos degradados.

3.  **Degradação de Recurso (`degradeFeature`):**
    *   Quando um recurso é degradado, seu estado é definido como `disabled`.
    *   Uma função de callback (`onFeatureToggle`) pode ser registrada para notificar outros componentes de que um recurso foi desabilitado.

4.  **Restauração de Recurso (`restoreFeatures`):**
    *   A função `restoreFeatures` é chamada após um atraso configurável quando o nível de contrapressão diminui.
    *   Ela verifica quais recursos podem ser reativados com base no nível de contrapressão atual e os restaura para o estado `enabled`.

## Papel e Importância

O módulo `pkg/degradation` é um componente chave para garantir a resiliência da aplicação sob alta carga. Seus principais papéis são:

*   **Degradação Graciosa:** Permite que a aplicação degrade graciosamente seu desempenho, desabilitando recursos não essenciais, em vez de falhar completamente.
*   **Priorização:** Permite que a aplicação priorize sua funcionalidade principal (ou seja, captura e entrega de logs), eliminando a carga de recursos menos críticos.
*   **Recuperação Automática:** Restaura automaticamente os recursos degradados quando a carga do sistema volta ao normal.

## Configurações

O módulo `degradation` é configurado através da seção `dispatcher.degradation_config` do arquivo `config.yaml`. As principais configurações incluem:

*   `degrade_at_low`, `degrade_at_medium`, `degrade_at_high`, `degrade_at_critical`: Essas listas definem quais recursos degradar em cada nível de contrapressão.
*   `grace_period`: A quantidade de tempo a esperar antes de degradar os recursos após uma mudança no nível de contrapressão.
*   `restore_delay`: A quantidade de tempo a esperar antes de restaurar os recursos após a diminuição do nível de contrapressão.

## Problemas e Melhorias

*   **Descoberta de Recursos:** A lista de recursos degradáveis está atualmente codificada. Uma abordagem mais dinâmica seria permitir que os componentes se registrassem como recursos degradáveis.
*   **Controle Mais Granular:** A implementação atual permite apenas habilitar ou desabilitar recursos. Uma implementação mais avançada poderia permitir um controle mais granular, como reduzir a qualidade de um recurso (por exemplo, reduzir a taxa de amostragem de métricas) em vez de desabilitá-lo completamente.
*   **Políticas Definidas pelo Usuário:** As políticas de degradação estão atualmente codificadas. Uma abordagem mais flexível seria permitir que os usuários definissem suas próprias políticas de degradação no arquivo de configuração.
