# Módulo Processing (internal/processing)

## Estrutura e Responsabilidades

O módulo `processing` é responsável pela **transformação e enriquecimento** de logs através de pipelines configuráveis. Este módulo aplica regras de processamento, extração de dados, normalização e enriquecimento antes que os logs sejam enviados para os destinos finais.

### Arquivos Principais:
- `log_processor.go` - Processador principal e orquestração de pipelines

## Funcionamento

### Arquitetura de Processamento:
```
[Raw Logs] -> [Pipeline Selection] -> [Step Processing] -> [Processed Logs]
     |              |                       |                    |
LogEntry      Source Mapping         Step Processors        Enhanced LogEntry
Labels        Pattern Matching       Transformations        Structured Data
Raw Message   Pipeline Router        Extractions            Normalized Fields
Metadata      Conditional Logic      Enrichment             Validated Data
```

### Conceitos Principais:

#### 1. **Pipelines**
- **Definição**: Sequências configuráveis de steps de processamento
- **Flexibilidade**: Cada pipeline pode ser customizado para diferentes tipos de log
- **Roteamento**: Logs são roteados para pipelines baseado em patterns de source

#### 2. **Processing Steps**
- **Modularidade**: Cada step executa uma transformação específica
- **Encadeamento**: Steps são executados sequencialmente no pipeline
- **Condicionalidade**: Steps podem ter condições para execução

#### 3. **Source Mapping**
- **Roteamento Inteligente**: Diferentes sources são mapeados para pipelines específicos
- **Pattern Matching**: Uso de patterns para identificar tipos de log
- **Default Pipeline**: Pipeline padrão para logs não mapeados

### Estrutura Principal:
```go
type LogProcessor struct {
    config        types.PipelineConfig
    pipelines     map[string]*Pipeline
    sourceMapping map[string][]string
    logger        *logrus.Logger
    mutex         sync.RWMutex
}

type Pipeline struct {
    Name         string
    Description  string
    Steps        []ProcessingStep
    SourceMap    map[string][]string
    compiledSteps []CompiledStep
}

type ProcessingStep struct {
    Name      string
    Type      string
    Config    map[string]interface{}
    Condition string
}

type CompiledStep struct {
    Step      ProcessingStep
    Processor StepProcessor
    Condition *regexp.Regexp
}
```

### Tipos de Step Processors:

#### 1. **regex_extract**
- **Propósito**: Extração de campos usando expressões regulares
- **Uso**: Parsing de logs estruturados ou semi-estruturados

#### 2. **timestamp_parse**
- **Propósito**: Parsing e normalização de timestamps
- **Uso**: Conversão de formatos de timestamp diversos

#### 3. **json_parse**
- **Propósito**: Parsing de mensagens JSON
- **Uso**: Extração de campos de logs JSON estruturados

#### 4. **field_add**
- **Propósito**: Adição de campos estáticos ou computados
- **Uso**: Enriquecimento com metadados ou transformações

#### 5. **field_remove**
- **Propósito**: Remoção de campos desnecessários
- **Uso**: Limpeza e redução de ruído

#### 6. **log_level_extract**
- **Propósito**: Extração e normalização de níveis de log
- **Uso**: Padronização de levels (INFO, WARN, ERROR, etc.)

## Papel e Importância

### Transformação de Dados:
- **Normalização**: Padronização de formatos diferentes para formato comum
- **Estruturação**: Conversão de logs não estruturados em dados estruturados
- **Enriquecimento**: Adição de contexto e metadados relevantes

### Otimização:
- **Filtering**: Remoção de dados desnecessários para reduzir volume
- **Compression**: Preparação de dados para compressão eficiente
- **Performance**: Processamento otimizado para alta throughput

### Qualidade de Dados:
- **Validation**: Validação de formato e conteúdo
- **Sanitization**: Limpeza de dados sensíveis ou inválidos
- **Consistency**: Garantia de consistência entre diferentes sources

## Configurações Aplicáveis

### Configuração Principal:
```yaml
processing:
  enabled: true
  file: "pipeline_config.yml"

  # Performance settings
  worker_pool_size: 4
  queue_size: 1000
  timeout: "5s"

  # Error handling
  error_handling:
    strategy: "continue"  # continue, stop, dlq
    max_retries: 3
    retry_delay: "1s"
```

### Arquivo de Pipeline (pipeline_config.yml):
```yaml
# Source mapping global
source_mapping:
  web_logs:
    - "container:nginx"
    - "container:apache"
    - "file:/var/log/nginx"

  app_logs:
    - "container:web-api"
    - "container:backend"
    - "file:/app/logs"

  system_logs:
    - "file:/var/log/syslog"
    - "file:/var/log/messages"

# Pipeline definitions
pipelines:
  - name: "web_logs"
    description: "Processing for web server logs"
    steps:
      - name: "extract_nginx_fields"
        type: "regex_extract"
        condition: "nginx"
        config:
          pattern: '(?P<ip>\d+\.\d+\.\d+\.\d+) - - \[(?P<timestamp>[^\]]+)\] "(?P<method>\w+) (?P<path>[^"]*)" (?P<status>\d+) (?P<size>\d+)'
          fields:
            - "ip"
            - "timestamp"
            - "method"
            - "path"
            - "status"
            - "size"

      - name: "parse_timestamp"
        type: "timestamp_parse"
        config:
          field: "timestamp"
          format: "02/Jan/2006:15:04:05 -0700"
          target_field: "parsed_timestamp"

      - name: "add_log_type"
        type: "field_add"
        config:
          fields:
            log_type: "access_log"
            service: "web_server"

  - name: "app_logs"
    description: "Processing for application logs"
    steps:
      - name: "parse_json"
        type: "json_parse"
        condition: "^\\{"
        config:
          target_prefix: "app_"
          preserve_original: false

      - name: "extract_log_level"
        type: "log_level_extract"
        config:
          field: "app_level"
          target_field: "level"
          normalize: true

      - name: "remove_sensitive"
        type: "field_remove"
        config:
          fields:
            - "app_password"
            - "app_token"
            - "app_secret"

  - name: "default"
    description: "Default processing pipeline"
    steps:
      - name: "add_defaults"
        type: "field_add"
        config:
          fields:
            processed: "true"
            pipeline: "default"
```

### Configuração de Step Específico:

#### Regex Extract:
```yaml
- name: "extract_custom_fields"
  type: "regex_extract"
  config:
    pattern: 'User: (?P<user>\w+), Action: (?P<action>\w+), Result: (?P<result>\w+)'
    fields:
      - "user"
      - "action"
      - "result"
    fail_on_no_match: false
    preserve_original: true
```

#### Timestamp Parse:
```yaml
- name: "parse_custom_timestamp"
  type: "timestamp_parse"
  config:
    field: "timestamp_string"
    formats:
      - "2006-01-02T15:04:05Z"
      - "2006-01-02 15:04:05"
      - "Jan _2 15:04:05"
    target_field: "parsed_timestamp"
    timezone: "UTC"
    fail_on_parse_error: false
```

#### JSON Parse:
```yaml
- name: "parse_json_payload"
  type: "json_parse"
  config:
    field: "message"  # default field if not specified
    target_prefix: "json_"
    flatten: true
    max_depth: 3
    preserve_original: false
    ignore_parse_errors: true
```

## Problemas Conhecidos

### Performance:
- **Regex Overhead**: Expressões regulares complexas podem ser custosas
- **Memory Allocation**: Criação de muitos objetos durante processamento
- **Pipeline Bottlenecks**: Steps lentos podem afetar throughput global

### Configuração:
- **Complex Patterns**: Padrões de matching podem ser difíceis de manter
- **Pipeline Conflicts**: Conflitos entre diferentes pipelines
- **Configuration Drift**: Configurações podem divergir entre ambientes

### Error Handling:
- **Step Failures**: Falhas em um step podem afetar todo o pipeline
- **Data Loss**: Processamento incorreto pode causar perda de dados
- **Partial Processing**: Alguns logs podem ser processados parcialmente

## Melhorias Propostas

### Performance Optimization:
```go
type OptimizedProcessor struct {
    stepCache       *StepCache
    regexPool       *RegexPool
    workerPool      *WorkerPool
    batchProcessor  *BatchProcessor
}

type StepCache struct {
    compiledRegex   map[string]*regexp.Regexp
    parsers         map[string]Parser
    transformers    map[string]Transformer
}

func (op *OptimizedProcessor) ProcessBatch(entries []types.LogEntry) ([]types.LogEntry, error)
```

### Dynamic Pipeline Management:
```go
type DynamicPipelineManager struct {
    pipelines       map[string]*Pipeline
    runtimeCompiler *RuntimeCompiler
    hotReloader     *HotReloader
    validator       *PipelineValidator
}

func (dpm *DynamicPipelineManager) UpdatePipeline(name string, config PipelineConfig) error
func (dpm *DynamicPipelineManager) ValidatePipeline(config PipelineConfig) error
func (dpm *DynamicPipelineManager) ReloadFromFile(file string) error
```

### Advanced Step Types:
```go
// Machine Learning based processing
type MLProcessor struct {
    model       MLModel
    features    []string
    threshold   float64
}

// GeoIP enrichment
type GeoIPProcessor struct {
    database    *geoip.Database
    fields      []string
    target      string
}

// Template-based transformation
type TemplateProcessor struct {
    template    *template.Template
    context     map[string]interface{}
}
```

### Conditional Processing:
```go
type ConditionalEngine struct {
    conditions  map[string]Condition
    evaluator   *ExpressionEvaluator
}

type Condition interface {
    Evaluate(entry *types.LogEntry) bool
    Dependencies() []string
}

type ExpressionCondition struct {
    expression  string
    compiled    CompiledExpression
}

func (ce *ConditionalEngine) ShouldExecute(stepName string, entry *types.LogEntry) bool
```

### Pipeline Analytics:
```go
type PipelineAnalytics struct {
    stepMetrics     map[string]*StepMetrics
    pipelineMetrics map[string]*PipelineMetrics
    aggregator      *MetricsAggregator
}

type StepMetrics struct {
    ExecutionCount   int64
    SuccessCount     int64
    ErrorCount       int64
    AverageLatency   time.Duration
    ThroughputPerSec float64
}

func (pa *PipelineAnalytics) GetStepPerformance(stepName string) StepMetrics
func (pa *PipelineAnalytics) GenerateReport() AnalyticsReport
```

### Error Recovery:
```go
type ErrorRecovery struct {
    strategies  map[string]RecoveryStrategy
    dlq         *DeadLetterQueue
    retryPolicy *RetryPolicy
}

type RecoveryStrategy interface {
    Handle(err error, entry *types.LogEntry) RecoveryAction
}

type RecoveryAction int
const (
    Continue RecoveryAction = iota
    Retry
    DLQ
    Drop
    Stop
)
```

## Métricas Expostas

### Pipeline Metrics:
- `processing_pipeline_executions_total` - Total de execuções por pipeline
- `processing_pipeline_duration_seconds` - Duração de processamento por pipeline
- `processing_pipeline_errors_total` - Total de erros por pipeline

### Step Metrics:
- `processing_step_executions_total` - Total de execuções por step
- `processing_step_duration_seconds` - Duração por step
- `processing_step_errors_total` - Erros por step e tipo

### Performance Metrics:
- `processing_throughput_entries_per_second` - Throughput de processamento
- `processing_queue_size` - Tamanho da fila de processamento
- `processing_active_workers` - Workers ativos

## Exemplo de Uso

### Setup Básico:
```go
// Configuração
pipelineConfig := types.PipelineConfig{
    Enabled: true,
    File:    "pipeline_config.yml",
}

// Criação
processor, err := processing.NewLogProcessor(pipelineConfig, logger)
if err != nil {
    log.Fatal(err)
}

// Processamento
entry := &types.LogEntry{
    Timestamp:  time.Now(),
    Message:    "192.168.1.1 - - [25/Dec/2023:10:00:00 +0000] \"GET /api/users\" 200 1234",
    SourceType: "container",
    Labels: map[string]string{
        "container_name": "nginx",
    },
}

processedEntry, err := processor.Process(ctx, entry)
if err != nil {
    log.Printf("Processing error: %v", err)
}
```

### Pipeline Customizado:
```go
// Criar step customizado
type CustomProcessor struct {
    config map[string]interface{}
}

func (cp *CustomProcessor) Process(ctx context.Context, entry *types.LogEntry) (*types.LogEntry, error) {
    // Implementar lógica customizada
    entry.Labels["custom_field"] = "custom_value"
    return entry, nil
}

func (cp *CustomProcessor) GetType() string {
    return "custom_transform"
}
```

## Dependências

### Bibliotecas Externas:
- `gopkg.in/yaml.v2` - Parsing de configuração YAML
- `github.com/sirupsen/logrus` - Logging estruturado

### Módulos Internos:
- `internal/metrics` - Para exposição de métricas de processamento
- `pkg/types` - Para tipos de dados de log
- `pkg/validation` - Para validação de dados processados

O módulo `processing` é o **cérebro** do sistema de logs, responsável por transformar dados brutos em informações estruturadas e úteis. Sua flexibilidade e performance são críticas para o valor agregado que o sistema pode fornecer aos usuários finais.