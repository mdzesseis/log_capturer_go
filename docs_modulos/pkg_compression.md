# Módulo: pkg/compression

## Estrutura

*   `http_compression.go`: Este arquivo contém o `HTTPCompressionManager` e implementações de diferentes algoritmos de compressão (`gzip`, `zstd`).
*   `http_compressor.go`: Este arquivo contém o `HTTPCompressor`, que é um gerenciador de compressão mais avançado e configurável.

## Como funciona

O módulo `pkg/compression` fornece um mecanismo para comprimir dados, o que é particularmente útil para reduzir o tamanho dos lotes de log antes de enviá-los para coletores remotos.

1.  **Inicialização (`NewHTTPCompressor`):**
    *   Cria uma nova instância de `HTTPCompressor`.
    *   Define valores padrão para o algoritmo de compressão, tamanho mínimo de dados para comprimir e nível de compressão.
    *   Inicializa pools de gravadores de compressão para diferentes algoritmos para melhorar o desempenho, reutilizando recursos.

2.  **Compressão (`Compress`):**
    *   A função `Compress` recebe uma fatia de bytes de dados, um algoritmo de compressão desejado e um tipo de coletor.
    *   Primeiro, ela verifica se os dados são grandes o suficiente para valer a pena comprimir.
    *   Em seguida, verifica se há alguma configuração de compressão específica do coletor.
    *   Se o algoritmo for definido como `auto`, ele seleciona o algoritmo ideal com base no tamanho dos dados.
    *   Em seguida, usa o gravador de compressão apropriado do pool para comprimir os dados.

3.  **Descompressão (`Decompress`):**
    *   A função `Decompress` recebe uma fatia de bytes de dados comprimidos e o algoritmo de compressão e retorna os dados descomprimidos.

4.  **Algoritmos:**
    *   O módulo suporta vários algoritmos de compressão, incluindo `gzip`, `zlib`, `zstd`, `lz4` e `snappy`.

## Papel e Importância

O módulo `pkg/compression` é importante para melhorar a eficiência do pipeline de entrega de logs. Seus principais papéis são:

*   **Redução de Largura de Banda:** Ao comprimir os lotes de log, ele reduz significativamente a quantidade de dados que precisam ser enviados pela rede, o que pode levar a economia de custos e melhor desempenho, especialmente para registro de alto volume.
*   **Melhora de Desempenho:** Embora a compressão adicione alguma sobrecarga de CPU, a redução no tempo de transferência de rede pode muitas vezes resultar em uma melhoria líquida de desempenho, especialmente para coletores remotos com alta latência.
*   **Flexibilidade:** Suporta múltiplos algoritmos de compressão, permitindo ao usuário escolher o melhor para suas necessidades específicas (por exemplo, `gzip` para boa compressão, `lz4` para alta velocidade).

## Configurações

O módulo `compression` é configurado através da seção `compression` da configuração de um coletor no arquivo `config.yaml`. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita a compressão para aquele coletor.
*   `algorithm`: O algoritmo de compressão a ser usado.
*   `level`: O nível de compressão (para algoritmos que o suportam).

## Problemas e Melhorias

*   **Redundância:** Existem dois arquivos semelhantes, `http_compression.go` e `http_compressor.go`, que parecem ter funcionalidades sobrepostas. Isso poderia ser consolidado em uma única implementação mais coesa.
*   **Compressão Adaptativa:** A flag `AdaptiveEnabled` na configuração sugere que o módulo pode escolher adaptativamente o melhor algoritmo de compressão, mas a implementação desse recurso não está totalmente clara no código. Isso poderia ser melhorado e mais bem documentado.
*   **Compressão de Streaming:** A implementação atual comprime o lote inteiro de uma vez. Para lotes muito grandes, uma abordagem de compressão de streaming poderia ser mais eficiente em termos de memória.
