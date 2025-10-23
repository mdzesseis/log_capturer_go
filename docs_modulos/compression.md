# Módulo Compression

## Estrutura e Operação

O módulo `compression` oferece uma camada de compressão de dados para otimizar o envio de logs pela rede. Ele suporta múltiplos algoritmos de compressão e é projetado para ser eficiente, utilizando pools de compressores para minimizar a alocação de memória.

### Principais Componentes da Estrutura:

- **`HTTPCompressor`**: A estrutura principal que gerencia a lógica de compressão. Ela pode ser configurada para usar diferentes algoritmos e possui um pool de "writers" para cada um.
- **`Compressor` Interface**: Uma interface que define os métodos básicos para um algoritmo de compressão (`Compress`, `ContentEncoding`).
- **Implementações de Algoritmos**: O módulo inclui implementações para vários algoritmos de compressão, como `Gzip`, `Zstd`, `Zlib`, `LZ4` e `Snappy`.
- **`compressionPool`**: Uma estrutura interna que gerencia pools de `writers` de compressão reutilizáveis, melhorando a performance ao evitar a criação e destruição constantes de objetos.

### Fluxo de Operação:

1.  **Seleção do Algoritmo**: Quando um `sink` precisa enviar um lote de logs, ele passa os dados para o `HTTPCompressor`.
2.  **Seleção Automática (Opcional)**: Se a compressão adaptativa estiver habilitada, o `HTTPCompressor` pode selecionar o algoritmo mais apropriado com base no tamanho dos dados e nos algoritmos suportados pelo servidor de destino (através do header `Accept-Encoding`).
3.  **Compressão**: O `HTTPCompressor` obtém um `writer` do pool correspondente ao algoritmo selecionado e comprime os dados.
4.  **Otimização**: A compressão só é aplicada se o resultado for de fato menor que os dados originais e se o tamanho dos dados ultrapassar um `threshold` mínimo (`min_bytes`).
5.  **Retorno**: O resultado da compressão, incluindo os dados comprimidos e o `Content-Encoding` header apropriado, é retornado ao `sink` para ser incluído na requisição HTTP.

## Papel e Importância

O módulo `compression` desempenha um papel **crucial na otimização do uso de rede e na redução de custos**. Ao comprimir os lotes de logs antes de enviá-los, ele oferece os seguintes benefícios:

- **Redução de Banda**: Diminui significativamente a quantidade de dados trafegados pela rede, o que é especialmente importante em ambientes com alta volumetria de logs ou em redes com custo por tráfego.
- **Melhora na Performance**: Payloads menores são transmitidos mais rapidamente, reduzindo a latência de envio para os `sinks`.
- **Eficiência de Custo**: Em serviços de nuvem, a redução do tráfego de saída (egress) pode levar a uma economia de custos significativa.
- **Flexibilidade**: O suporte a múltiplos algoritmos permite escolher o melhor balanço entre taxa de compressão e uso de CPU para cada caso de uso.

## Configurações Aplicáveis

As configurações de compressão podem ser definidas globalmente ou por `sink` no arquivo `config.yaml`:

- **`enabled`**: Habilita ou desabilita a compressão para um `sink` específico.
- **`default_algorithm`**: O algoritmo de compressão a ser usado por padrão (ex: `gzip`, `zstd`).
- **`adaptive_enabled`**: Habilita a seleção automática do melhor algoritmo.
- **`min_bytes`**: O tamanho mínimo (em bytes) que um payload deve ter para que a compressão seja aplicada.
- **`level`**: O nível de compressão a ser usado (varia para cada algoritmo).

## Problemas e Melhorias

### Problemas Potenciais:

- **Overhead de CPU**: A compressão, especialmente em níveis mais altos, consome ciclos de CPU. Em sistemas com CPU limitada, isso pode se tornar um gargalo e afetar a performance geral da aplicação.
- **Seleção de Algoritmo**: A escolha do algoritmo ideal é um trade-off. Algoritmos como `zstd` oferecem ótima compressão, mas podem usar mais CPU. Algoritmos como `lz4` são extremamente rápidos, mas com uma taxa de compressão menor.

### Sugestões de Melhorias:

- **Benchmarking Dinâmico**: O `HTTPCompressor` poderia realizar benchmarks dinâmicos em segundo plano para determinar qual algoritmo oferece o melhor balanço entre compressão e uso de CPU para o perfil de dados atual.
- **Compressão em Nível de Stream**: Em vez de comprimir o lote inteiro de uma vez na memória, a compressão poderia ser feita em modo de streaming, reduzindo o pico de uso de memória.
- **Suporte a Novos Algoritmos**: Manter o módulo atualizado com novos e mais eficientes algoritmos de compressão que possam surgir.
- **Métricas de Compressão por Sink**: Expor métricas detalhadas de compressão (taxa, latência) para o Prometheus com um label por `sink`, permitindo uma análise mais granular da eficiência da compressão para cada destino.
