# Módulo Hot Reload

## Estrutura e Operação

O módulo `hotreload` (implementado como `ConfigReloader`) fornece a capacidade de recarregar as configurações da aplicação em tempo de execução, sem a necessidade de reiniciar o serviço. Ele monitora os arquivos de configuração em busca de alterações e, quando as detecta, aciona um processo de recarregamento.

### Principais Componentes da Estrutura:

- **`ConfigReloader`**: A estrutura principal que gerencia o monitoramento de arquivos e o processo de recarregamento.
- **`fsnotify.Watcher`**: Uma biblioteca externa utilizada para monitorar eventos do sistema de arquivos (como escrita, criação ou renomeação de arquivos) de forma eficiente.
- **Callbacks**: O `ConfigReloader` utiliza um sistema de callbacks para notificar outros módulos sobre as mudanças na configuração:
    - `onConfigChanged`: Chamado para que os componentes possam aplicar as novas configurações.
    - `onReloadSuccess`: Chamado após um recarregamento bem-sucedido.
    - `onReloadError`: Chamado se ocorrer um erro durante o processo.
- **Debouncing**: Implementa uma lógica de "debounce" para evitar múltiplos recarregamentos em um curto período de tempo se ocorrerem várias escritas rápidas no arquivo.

### Fluxo de Operação:

1.  **Monitoramento**: O `ConfigReloader` inicia um `watcher` que monitora o arquivo de configuração principal (`config.yaml`) e quaisquer outros arquivos especificados.
2.  **Detecção de Mudança**: Quando o `watcher` detecta uma alteração em um dos arquivos monitorados, ele aguarda um curto `debounce_interval` para agrupar múltiplas alterações.
3.  **Recarregamento**: Após o debounce, o `performReload` é acionado:
    - **Backup (Opcional)**: Se configurado, ele primeiro cria um backup do arquivo de configuração atual.
    - **Carregamento**: O novo arquivo de configuração é lido e parseado.
    - **Validação**: A nova configuração é validada para garantir que é sintaticamente e semanticamente correta.
    - **Aplicação**: Se a validação for bem-sucedida, o callback `onConfigChanged` é chamado, passando a configuração antiga e a nova para os componentes da aplicação, que são responsáveis por aplicar as mudanças.
4.  **Notificação**: Os callbacks de sucesso ou erro são chamados para registrar o resultado da operação.

## Papel e Importância

O módulo `hotreload` é **extremamente importante para a operabilidade e a flexibilidade** do `log_capturer_go` em ambientes de produção.

Sua importância se deve a:

- **Zero Downtime**: Permite que os administradores do sistema alterem o comportamento da aplicação (como adicionar um novo `sink`, alterar um `pipeline` de processamento ou ajustar o nível de log) sem interromper o serviço.
- **Agilidade**: Facilita a experimentação e o ajuste fino das configurações em um ambiente de produção.
- **Segurança**: Ao validar a nova configuração antes de aplicá-la, ele reduz o risco de que uma configuração malformada cause uma falha na aplicação. O recurso de backup também oferece uma camada extra de segurança.

## Configurações Aplicáveis

As configurações para o `ConfigReloader` são definidas na seção `hot_reload` do `config.yaml`:

- **`enabled`**: Habilita ou desabilita a funcionalidade de hot reload.
- **`watch_interval`**: Um intervalo de verificação periódica adicional (baseado em hash do arquivo) como um fallback para o `fsnotify`.
- **`debounce_interval`**: O tempo de espera após a detecção de uma mudança antes de iniciar o recarregamento.
- **`watch_files`**: Uma lista de arquivos de configuração adicionais a serem monitorados (ex: `pipelines.yaml`).
- **`validate_on_reload`**: Se `true`, a nova configuração será validada antes de ser aplicada.
- **`backup_on_reload`**: Se `true`, um backup do arquivo de configuração será criado antes de cada recarregamento.

## Problemas e Melhorias

### Problemas Potenciais:

- **Aplicação Parcial de Configurações**: Nem todas as configurações podem ser alteradas em tempo de execução. Algumas (como a porta do servidor HTTP) exigem um reinício da aplicação. Se isso não for bem documentado, pode levar a confusão.
- **Complexidade nos Componentes**: Cada componente que suporta hot reload precisa implementar a lógica para lidar com a mudança de sua configuração em tempo de execução, o que pode adicionar complexidade ao seu código.
- **Condições de Corrida**: A alteração de configurações em tempo de execução em um sistema concorrente requer um cuidado especial com a sincronização para evitar condições de corrida.

### Sugestões de Melhorias:

- **Suporte a Mais Configurações Dinâmicas**: Expandir o número de componentes e configurações que podem ser alterados dinamicamente sem a necessidade de um reinício.
- **Rollback Automático**: Se a aplicação de uma nova configuração resultar em um estado não saudável (detectado por health checks), o sistema poderia tentar reverter automaticamente para a última configuração válida.
- **API para Reload**: Além do monitoramento de arquivos, adicionar um endpoint na API (ex: `POST /-/reload`) para acionar o recarregamento manualmente.
- **Notificações Detalhadas**: Enviar notificações (via webhook ou outro meio) com um "diff" das alterações de configuração sempre que um recarregamento ocorrer, para fins de auditoria.
