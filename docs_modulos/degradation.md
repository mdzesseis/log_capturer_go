# Módulo Degradation

## Estrutura e Operação

O módulo `degradation` implementa uma estratégia de degradação graciosa (graceful degradation), que desativa seletivamente funcionalidades não críticas da aplicação em resposta a altos níveis de `backpressure`. O objetivo é aliviar a carga do sistema e garantir que as funcionalidades principais (coleta e envio de logs) permaneçam operacionais.

### Principais Componentes da Estrutura:

- **`Manager` Struct**: A estrutura central que monitora o nível de `backpressure` e gerencia o estado (habilitado/desabilitado) de várias funcionalidades do sistema.
- **`Feature` Enum**: Define as funcionalidades que podem ser degradadas, como `FeatureDeduplication`, `FeatureProcessing`, `FeatureCompression`, etc.
- **`Config` Struct**: Contém a configuração que mapeia os níveis de `backpressure` às funcionalidades que devem ser desativadas em cada nível.

### Fluxo de Operação:

1.  **Monitoramento do Nível de Backpressure**: O `DegradationManager` é notificado pelo `BackpressureManager` sempre que o nível de contrapressão do sistema muda.
2.  **Avaliação de Degradação**: Quando o nível de `backpressure` aumenta, o `Manager` consulta sua configuração para determinar quais funcionalidades devem ser desativadas para aquele nível.
3.  **Desativação de Funcionalidades**: O `Manager` então desativa as funcionalidades selecionadas. Outros módulos da aplicação (como o `dispatcher`) consultam o `DegradationManager` (usando `IsFeatureEnabled()`) antes de executar uma operação para saber se devem ou não executá-la.
4.  **Restauração de Funcionalidades**: Quando o nível de `backpressure` diminui, o `Manager` não restaura as funcionalidades imediatamente. Ele aguarda um `RestoreDelay` para garantir que o sistema se estabilizou e, em seguida, reativa as funcionalidades que não são mais necessárias para a degradação no nível atual.

## Papel e Importância

O módulo `degradation` é uma camada avançada de resiliência, agindo em conjunto com o `backpressure`. Enquanto o `backpressure` gerencia o fluxo de entrada de dados, a degradação graciosa **reduz a carga de trabalho interna** da aplicação.

Sua importância reside em:

- **Priorização de Tarefas**: Garante que, sob estresse, os recursos da CPU e da memória sejam usados para as tarefas mais críticas (enviar logs) em detrimento de tarefas que consomem muitos recursos, mas são menos essenciais (como compressão ou processamento complexo).
- **Prevenção de Falhas**: Ao reduzir a carga de trabalho, ele ajuda a evitar que o sistema atinja um ponto de falha total devido à exaustão de recursos.
- **Manutenção da Funcionalidade Core**: Permite que a aplicação continue a executar sua função principal (mover logs do ponto A para o ponto B) da forma mais eficiente possível, mesmo que isso signifique sacrificar temporariamente o enriquecimento ou a otimização desses logs.

## Configurações Aplicáveis

As configurações para o `DegradationManager` são definidas na seção `degradation` do `config.yaml`:

- **`degrade_at_low`, `degrade_at_medium`, `degrade_at_high`, `degrade_at_critical`**: Listas de funcionalidades (`Feature`) a serem desativadas em cada nível de `backpressure`.
- **`grace_period`**: Um período de tempo que o sistema espera em um novo nível de `backpressure` antes de começar a desativar funcionalidades, para evitar ações precipitadas por picos de carga momentâneos.
- **`restore_delay`**: O tempo de espera após a redução do nível de `backpressure` antes de tentar reativar as funcionalidades.
- **`min_degraded_time`**: O tempo mínimo que uma funcionalidade deve permanecer desativada antes de poder ser reativada, para evitar oscilações.

## Problemas e Melhorias

### Problemas Potenciais:

- **Configuração Complexa**: Mapear as funcionalidades corretas para cada nível de degradação pode ser complexo e requer um bom entendimento do impacto de cada funcionalidade na performance.
- **Impacto no Dado**: Desativar o `processing` ou a `deduplication` afeta a qualidade e o conteúdo dos logs que chegam ao destino final. É um trade-off entre a qualidade do dado e a disponibilidade do serviço.
- **Efeito Cascata Inesperado**: Desativar uma funcionalidade pode ter efeitos colaterais inesperados em outras partes do sistema se as dependências não forem bem compreendidas.

### Sugestões de Melhorias:

- **Degradação Parcial**: Em vez de simplesmente ligar/desligar uma funcionalidade, implementar uma degradação parcial. Por exemplo, em vez de desativar toda a `compressão`, poderia-se reduzir o `nível` de compressão para um mais rápido e que consome menos CPU.
- **Análise de Custo de Funcionalidade**: Implementar um profiling que meça o custo (em CPU e memória) de cada funcionalidade para ajudar a tomar decisões mais informadas sobre o que degradar.
- **Controle Manual via API**: Adicionar endpoints na API que permitam a um administrador forçar manualmente a degradação ou a restauração de funcionalidades específicas para testes ou para responder a incidentes.
- **Políticas de Degradação Dinâmicas**: Permitir que as políticas de degradação sejam atualizadas em tempo de execução via `hot-reload`.
