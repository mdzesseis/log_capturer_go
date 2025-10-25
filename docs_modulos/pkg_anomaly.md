# Módulo: pkg/anomaly

## Estrutura

*   `detector.go`: Contém o `AnomalyDetector`, que é o componente principal para detectar anomalias em logs.
*   `extractors.go`: Define várias implementações de `FeatureExtractor` para extrair características de entradas de log.
*   `models.go`: Implementa os modelos de aprendizado de máquina usados para detecção de anomalias, como `IsolationForestModel`, `StatisticalModel` e `NeuralNetworkModel`.

## Como funciona

O módulo `pkg/anomaly` fornece um sistema de detecção de anomalias baseado em aprendizado de máquina para entradas de log.

1.  **Inicialização (`NewAnomalyDetector`):**
    *   Cria um novo `AnomalyDetector`.
    *   Inicializa os extratores de características (`TextFeatureExtractor`, `StatisticalFeatureExtractor`, etc.).
    *   Inicializa os modelos de aprendizado de máquina com base no algoritmo configurado (`isolation_forest`, `statistical`, `ml_ensemble`).

2.  **Extração de Características (`extractFeatures` e `extractors.go`):**
    *   Antes que uma entrada de log possa ser analisada, o `AnomalyDetector` usa um conjunto de `FeatureExtractor`s para converter os dados brutos do log em um conjunto de características numéricas.
    *   O arquivo `extractors.go` contém vários tipos de extratores:
        *   `TextFeatureExtractor`: Extrai características do texto da mensagem de log, como comprimento da mensagem, contagem de palavras e proporções de caracteres.
        *   `StatisticalFeatureExtractor`: Extrai características estatísticas, como a frequência de entradas de log de uma fonte específica ou com um determinado nível de log.
        *   `TemporalFeatureExtractor`: Extrai características baseadas no tempo, como o intervalo entre as entradas de log.
        *   `PatternFeatureExtractor`: Extrai características com base em padrões de expressão regular, como a presença de mensagens de erro ou palavras-chave relacionadas à segurança.

3.  **Detecção de Anomalias (`DetectAnomaly` e `models.go`):**
    *   A função `DetectAnomaly` pega uma entrada de log, extrai suas características e, em seguida, usa o modelo de aprendizado de máquina configurado para prever uma pontuação de anomalia.
    *   O arquivo `models.go` contém as implementações dos diferentes modelos de detecção de anomalias:
        *   `IsolationForestModel`: Um algoritmo de aprendizado não supervisionado que é eficaz na detecção de outliers.
        *   `StatisticalModel`: Um modelo que usa métodos estatísticos como z-scores e percentis para identificar anomalias.
        *   `NeuralNetworkModel`: Uma rede neural simples que pode ser treinada para reconstruir entradas de log normais e terá um alto erro de reconstrução para entradas anômalas.
    *   Se a pontuação de anomalia exceder um limiar configurado, a entrada de log é marcada como uma anomalia.

4.  **Treinamento (`trainModels`):**
    *   O `AnomalyDetector` retreina periodicamente seus modelos usando um buffer de entradas de log recentes. Isso permite que os modelos se adaptem a padrões de log em mudança ao longo do tempo.

## Papel e Importância

O módulo `pkg/anomaly` é um recurso poderoso que pode ajudar a identificar automaticamente entradas de log incomuns ou problemáticas. Seus principais papéis são:

*   **Detecção Proativa de Problemas:** Ele pode detectar anomalias que podem indicar uma violação de segurança, um problema de desempenho ou um bug de software, muitas vezes antes que esses problemas causem uma interrupção importante.
*   **Redução de Ruído:** Em um ambiente de registro de alto volume, pode ser difícil para os operadores humanos localizarem entradas de log importantes. O sistema de detecção de anomalias pode ajudar a destacar os logs mais interessantes e potencialmente problemáticos.
*   **Monitoramento Adaptativo:** O mecanismo de treinamento online permite que o sistema se adapte às mudanças nos dados de log, reduzindo a necessidade de ajuste manual das regras de alerta.

## Configurações

A seção `anomaly_detection` do arquivo `config.yaml` é usada para configurar este módulo. As principais configurações incluem:

*   `enabled`: Habilita ou desabilita a detecção de anomalias.
*   `algorithm`: O algoritmo de aprendizado de máquina a ser usado (`isolation_forest`, `statistical`, `ml_ensemble`).
*   `sensitivity_level`: A sensibilidade do algoritmo de detecção.
*   `window_size`: A janela de tempo para análise.
*   `min_samples`: O número mínimo de entradas de log necessárias para o treinamento.
*   `model_path`: O diretório onde os modelos treinados são salvos.
*   `training_enabled`: Habilita ou desabilita o treinamento online do modelo.

## Problemas e Melhorias

*   **Complexidade do Modelo:** Os modelos de aprendizado de máquina atuais são relativamente simples. Modelos mais avançados, como LSTMs ou transformadores, poderiam ser usados para uma detecção de anomalias mais sofisticada.
*   **Engenharia de Características:** A qualidade da detecção de anomalias é altamente dependente da qualidade das características. Técnicas mais avançadas de engenharia de características poderiam ser usadas para extrair características mais significativas dos dados de log.
*   **Explicabilidade:** O sistema atual fornece um "motivo" básico para uma anomalia, mas poderia ser aprimorado para fornecer explicações mais detalhadas sobre por que uma entrada de log específica foi marcada como anômala.
*   **Aprendizado Supervisionado:** O sistema atual não é supervisionado, o que significa que não requer dados rotulados. No entanto, se um conjunto de dados de anomalias rotuladas estivesse disponível, uma abordagem de aprendizado supervisionado poderia ser usada para alcançar maior precisão.
