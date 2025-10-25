# Módulo: pkg/cleanup

## Estrutura

*   `disk_manager.go`: Este arquivo contém o `DiskSpaceManager`, que é responsável por limpar automaticamente arquivos antigos e grandes para evitar que o disco fique cheio.
*   `disk_manager_test.go`: Contém testes unitários para o `DiskSpaceManager`.

## Como funciona

O módulo `pkg/cleanup` fornece um mecanismo para gerenciar automaticamente o uso do espaço em disco.

1.  **Inicialização (`NewDiskSpaceManager`):**
    *   Cria uma nova instância do `DiskSpaceManager` com a configuração especificada.
    *   A configuração define quais diretórios monitorar, as políticas de limpeza (por idade, tamanho e contagem) e com que frequência verificar a limpeza.

2.  **Loop de Monitoramento (`monitorLoop`):**
    *   O método `Start` inicia uma goroutine em segundo plano que executa o `monitorLoop`.
    *   Este loop chama periodicamente `performCleanup` no intervalo definido na configuração.

3.  **Limpeza (`performCleanup` e `cleanupDirectory`):**
    *   A função `performCleanup` itera sobre todos os diretórios configurados и chama `cleanupDirectory` para cada um.
    *   A função `cleanupDirectory` aplica três tipos de políticas de limpeza em ordem:
        *   `cleanupByAge`: Exclui arquivos mais antigos que o período de retenção configurado.
        *   `cleanupBySize`: Se o tamanho total dos arquivos no diretório exceder o limite configurado, ele exclui os arquivos mais antigos até que o tamanho total esteja dentro do limite.
        *   `cleanupByCount`: Se o número de arquivos no diretório exceder o limite configurado, ele exclui os arquivos mais antigos até que o número de arquivos esteja dentro do limite.

4.  **Verificação de Espaço em Disco (`checkDiskSpace`):**
    *   Além de limpar diretórios individuais, o `DiskSpaceManager` também verifica periodicamente o uso geral do espaço em disco.
    *   Se o uso do espaço em disco exceder um limiar de aviso ou crítico, ele registra uma mensagem para alertar o operador.

## Papel e Importância

O módulo `pkg/cleanup` é essencial para a estabilidade e confiabilidade a longo prazo da aplicação `log_capturer_go`. Seus principais papéis são:

*   **Prevenção do Esgotamento do Disco:** Ao excluir automaticamente arquivos antigos e grandes, evita que o disco fique cheio, o que poderia causar a falha da aplicação e de outros serviços no sistema.
*   **Rotação de Logs:** Fornece um mecanismo básico de rotação de logs, garantindo que os arquivos de log não cresçam indefinidamente.
*   **Manutenção Automatizada:** Automatiza o processo de limpeza de dados antigos, reduzindo a necessidade de intervenção manual.

## Configurações

O módulo `cleanup` é configurado através da seção `cleanup` do arquivo `config.yaml`. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita o gerenciador de limpeza de disco.
*   `check_interval`: Com que frequência verificar a limpeza.
*   `directories`: Uma lista de diretórios a serem monitorados, cada um com suas próprias políticas de limpeza:
    *   `path`: O caminho para o diretório.
    *   `max_size_mb`: O tamanho total máximo dos arquivos no diretório.
    *   `retention_days`: A idade máxima dos arquivos a serem mantidos.
    *   `max_files`: O número máximo de arquivos a serem mantidos.

## Problemas e Melhorias

*   **Estratégia de Limpeza:** A estratégia de limpeza atual é excluir primeiro os arquivos mais antigos. Uma estratégia mais sofisticada poderia ser excluir primeiro os arquivos maiores, ou usar uma combinação de idade e tamanho.
*   **Modo de Simulação (Dry Run):** Um modo de "simulação" que simula uma limpeza sem realmente excluir nenhum arquivo seria um recurso útil para testes e configuração.
*   **Políticas Mais Granulares:** As políticas de limpeza poderiam ser mais granulares. Por exemplo, seria possível configurar diferentes períodos de retenção para diferentes padrões de arquivo no mesmo diretório.
*   **Integração com Coletores (Sinks):** O `DiskSpaceManager` poderia ser mais integrado com os coletores. Por exemplo, ele poderia ser configurado para limpar automaticamente os diretórios de saída do `local_file_sink`.
