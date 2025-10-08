package processing

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// LogProcessor processa logs através de pipelines configuráveis
type LogProcessor struct {
	config        types.PipelineConfig
	pipelines     map[string]*Pipeline
	sourceMapping map[string][]string
	logger        *logrus.Logger
	mutex         sync.RWMutex // Protege pipelines e sourceMapping
}

// Pipeline representa um pipeline de processamento
type Pipeline struct {
	Name         string              `yaml:"name"`
	Description  string              `yaml:"description"`
	Steps        []ProcessingStep    `yaml:"steps"`
	SourceMap    map[string][]string `yaml:"source_mapping"`
	compiledSteps []CompiledStep
}

// ProcessingStep representa um passo de processamento
type ProcessingStep struct {
	Name      string                 `yaml:"name"`
	Type      string                 `yaml:"type"`
	Config    map[string]interface{} `yaml:"config"`
	Condition string                 `yaml:"condition,omitempty"`
}

// CompiledStep representa um passo compilado
type CompiledStep struct {
	Step      ProcessingStep
	Processor StepProcessor
	Condition *regexp.Regexp
}

// StepProcessor interface para processadores de steps
type StepProcessor interface {
	Process(ctx context.Context, entry *types.LogEntry) (*types.LogEntry, error)
	GetType() string
}

// PipelineConfig estrutura do arquivo de configuração
type PipelineConfig struct {
	Pipelines     []Pipeline            `yaml:"pipelines"`
	SourceMapping map[string][]string   `yaml:"source_mapping"`
}

// NewLogProcessor cria um novo processador de logs
func NewLogProcessor(config types.PipelineConfig, logger *logrus.Logger) (*LogProcessor, error) {
	processor := &LogProcessor{
		config:        config,
		pipelines:     make(map[string]*Pipeline),
		sourceMapping: make(map[string][]string),
		logger:        logger,
	}

	if config.Enabled && config.File != "" {
		if err := processor.loadPipelines(); err != nil {
			return nil, fmt.Errorf("failed to load pipelines: %w", err)
		}
	}

	return processor, nil
}

// loadPipelines carrega pipelines do arquivo de configuração
func (lp *LogProcessor) loadPipelines() error {
	data, err := os.ReadFile(lp.config.File)
	if err != nil {
		return fmt.Errorf("failed to read pipeline config file: %w", err)
	}

	var config PipelineConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse pipeline config: %w", err)
	}

	// Compilar pipelines
	for _, pipeline := range config.Pipelines {
		compiled, err := lp.compilePipeline(pipeline)
		if err != nil {
			return fmt.Errorf("failed to compile pipeline %s: %w", pipeline.Name, err)
		}
		lp.pipelines[pipeline.Name] = compiled
	}

	// Armazenar source mapping
	lp.sourceMapping = config.SourceMapping

	lp.logger.WithField("pipelines", len(lp.pipelines)).Info("Pipelines loaded successfully")
	return nil
}

// compilePipeline compila um pipeline
func (lp *LogProcessor) compilePipeline(pipeline Pipeline) (*Pipeline, error) {
	compiled := &Pipeline{
		Name:          pipeline.Name,
		Description:   pipeline.Description,
		Steps:         pipeline.Steps,
		SourceMap:     pipeline.SourceMap,
		compiledSteps: make([]CompiledStep, 0, len(pipeline.Steps)),
	}

	for _, step := range pipeline.Steps {
		compiledStep, err := lp.compileStep(step)
		if err != nil {
			return nil, fmt.Errorf("failed to compile step %s: %w", step.Name, err)
		}
		compiled.compiledSteps = append(compiled.compiledSteps, compiledStep)
	}

	return compiled, nil
}

// compileStep compila um step
func (lp *LogProcessor) compileStep(step ProcessingStep) (CompiledStep, error) {
	// Criar processor baseado no tipo
	var processor StepProcessor
	var err error

	switch step.Type {
	case "regex_extract":
		processor, err = NewRegexExtractProcessor(step.Config)
	case "timestamp_parse":
		processor, err = NewTimestampParseProcessor(step.Config)
	case "json_parse":
		processor, err = NewJSONParseProcessor(step.Config)
	case "field_add":
		processor, err = NewFieldAddProcessor(step.Config)
	case "field_remove":
		processor, err = NewFieldRemoveProcessor(step.Config)
	case "log_level_extract":
		processor, err = NewLogLevelExtractProcessor(step.Config)
	default:
		return CompiledStep{}, fmt.Errorf("unknown step type: %s", step.Type)
	}

	if err != nil {
		return CompiledStep{}, err
	}

	compiledStep := CompiledStep{
		Step:      step,
		Processor: processor,
	}

	// Compilar condição se existir
	if step.Condition != "" {
		regex, err := regexp.Compile(step.Condition)
		if err != nil {
			return CompiledStep{}, fmt.Errorf("failed to compile condition regex: %w", err)
		}
		compiledStep.Condition = regex
	}

	return compiledStep, nil
}

// Process processa uma entrada de log
func (lp *LogProcessor) Process(ctx context.Context, entry *types.LogEntry) (*types.LogEntry, error) {
	if !lp.config.Enabled {
		return entry, nil
	}

	// Encontrar pipeline apropriado
	pipeline := lp.findPipeline(entry)
	if pipeline == nil {
		// Usar pipeline padrão se disponível
		if defaultPipeline, exists := lp.pipelines["default"]; exists {
			pipeline = defaultPipeline
		} else {
			return entry, nil
		}
	}

	// Processar através do pipeline
	return lp.processThroughPipeline(ctx, entry, pipeline)
}

// findPipeline encontra o pipeline apropriado para uma entrada
func (lp *LogProcessor) findPipeline(entry *types.LogEntry) *Pipeline {
	lp.mutex.RLock()
	defer lp.mutex.RUnlock()

	// Verificar mapeamento global de sources para pipelines
	for pipelineName, sourcePatterns := range lp.sourceMapping {
		pipeline, exists := lp.pipelines[pipelineName]
		if !exists {
			continue
		}

		for _, pattern := range sourcePatterns {
			if lp.matchesSource(entry, pattern) {
				return pipeline
			}
		}
	}

	// Retornar pipeline padrão se existir
	if defaultPipeline, exists := lp.pipelines["default"]; exists {
		return defaultPipeline
	}

	return nil
}

// matchesSource verifica se a entrada corresponde ao padrão de source
func (lp *LogProcessor) matchesSource(entry *types.LogEntry, pattern string) bool {
	// Implementar matching baseado em source_type, source_id, etc.
	if strings.Contains(pattern, entry.SourceType) {
		return true
	}

	if containerName, exists := entry.Labels["container_name"]; exists {
		if strings.Contains(pattern, containerName) {
			return true
		}
	}

	return false
}

// matchesLabels verifica se a entrada corresponde aos labels do pipeline
func (lp *LogProcessor) matchesLabels(entry *types.LogEntry, pipeline *Pipeline) bool {
	// Implementar matching baseado em labels específicos
	return false
}

// processThroughPipeline processa entrada através de um pipeline
func (lp *LogProcessor) processThroughPipeline(ctx context.Context, entry *types.LogEntry, pipeline *Pipeline) (*types.LogEntry, error) {
	startTime := time.Now()
	currentEntry := entry

	for _, compiledStep := range pipeline.compiledSteps {
		// Verificar condição se existir
		if compiledStep.Condition != nil {
			if !compiledStep.Condition.MatchString(currentEntry.Message) {
				continue
			}
		}

		// Processar step
		processedEntry, err := compiledStep.Processor.Process(ctx, currentEntry)
		if err != nil {
			lp.logger.WithError(err).WithFields(logrus.Fields{
				"pipeline": pipeline.Name,
				"step":     compiledStep.Step.Name,
			}).Error("Step processing failed")
			return currentEntry, err
		}

		if processedEntry != nil {
			currentEntry = processedEntry
		}
	}

	// Atualizar timestamp de processamento
	currentEntry.ProcessedAt = time.Now()

	// Métricas
	duration := time.Since(startTime)
	lp.logger.WithFields(logrus.Fields{
		"pipeline":    pipeline.Name,
		"duration_ms": duration.Milliseconds(),
	}).Debug("Pipeline processing completed")

	return currentEntry, nil
}

// GetPipelineName retorna o nome do pipeline (implementa interface Processor)
func (lp *LogProcessor) GetPipelineName() string {
	return "log_processor"
}

// Implementações dos processadores de steps

// RegexExtractProcessor extrai campos usando regex
type RegexExtractProcessor struct {
	Pattern *regexp.Regexp
	Fields  []string
}

func NewRegexExtractProcessor(config map[string]interface{}) (*RegexExtractProcessor, error) {
	patternStr, ok := config["pattern"].(string)
	if !ok {
		return nil, fmt.Errorf("pattern is required for regex_extract")
	}

	pattern, err := regexp.Compile(patternStr)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	fieldsInterface, ok := config["fields"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("fields is required for regex_extract")
	}

	fields := make([]string, len(fieldsInterface))
	for i, field := range fieldsInterface {
		fields[i] = field.(string)
	}

	return &RegexExtractProcessor{
		Pattern: pattern,
		Fields:  fields,
	}, nil
}

func (rep *RegexExtractProcessor) Process(ctx context.Context, entry *types.LogEntry) (*types.LogEntry, error) {
	matches := rep.Pattern.FindStringSubmatch(entry.Message)
	if len(matches) > 1 {
		// Criar nova entrada com campos extraídos
		newEntry := *entry
		if newEntry.Labels == nil {
			newEntry.Labels = make(map[string]string)
		}

		for i, field := range rep.Fields {
			if i+1 < len(matches) {
				newEntry.Labels[field] = matches[i+1]
			}
		}

		return &newEntry, nil
	}

	return entry, nil
}

func (rep *RegexExtractProcessor) GetType() string {
	return "regex_extract"
}

// TimestampParseProcessor parseia timestamps com múltiplos formatos
type TimestampParseProcessor struct {
	Formats        []string
	Field          string
	TargetField    string
	UseAsLogTime   bool
	AutoDetect     bool
	TimeZone       *time.Location
}

func NewTimestampParseProcessor(config map[string]interface{}) (*TimestampParseProcessor, error) {
	processor := &TimestampParseProcessor{
		Field:       "message",
		TargetField: "timestamp",
		AutoDetect:  false,
		TimeZone:    time.UTC,
	}

	// Campo de origem
	if f, ok := config["field"].(string); ok {
		processor.Field = f
	}

	// Campo de destino
	if tf, ok := config["target_field"].(string); ok {
		processor.TargetField = tf
	}

	// Se deve usar como timestamp principal do log
	if ual, ok := config["use_as_log_timestamp"].(bool); ok {
		processor.UseAsLogTime = ual
	}

	// Timezone
	if tz, ok := config["timezone"].(string); ok {
		if loc, err := time.LoadLocation(tz); err == nil {
			processor.TimeZone = loc
		}
	}

	// Formatos de timestamp
	if format, ok := config["format"].(string); ok {
		processor.Formats = []string{format}
	} else if formatsInterface, ok := config["formats"].([]interface{}); ok {
		processor.Formats = make([]string, len(formatsInterface))
		for i, f := range formatsInterface {
			processor.Formats[i] = f.(string)
		}
	} else if autoDetect, ok := config["auto_detect"].(bool); ok && autoDetect {
		processor.AutoDetect = true
		processor.Formats = getCommonTimestampFormats()
	} else {
		return nil, fmt.Errorf("format, formats, or auto_detect is required for timestamp_parse")
	}

	return processor, nil
}

func (tpp *TimestampParseProcessor) Process(ctx context.Context, entry *types.LogEntry) (*types.LogEntry, error) {
	var value string
	if tpp.Field == "message" {
		value = entry.Message
	} else if labelValue, exists := entry.Labels[tpp.Field]; exists {
		value = labelValue
	} else {
		return entry, nil
	}

	// Tentar parsear timestamp com múltiplos formatos
	var parsedTime time.Time
	var err error

	for _, format := range tpp.Formats {
		parsedTime, err = time.ParseInLocation(format, value, tpp.TimeZone)
		if err == nil {
			break
		}
	}

	// Se auto-detect está habilitado, tentar formatos adicionais
	if err != nil && tpp.AutoDetect {
		parsedTime, err = tpp.autoDetectTimestamp(value)
	}

	if err != nil {
		return entry, nil // Não falhar se não conseguir parsear
	}

	// Criar nova entrada
	newEntry := *entry
	if newEntry.Labels == nil {
		newEntry.Labels = make(map[string]string)
	}

	// Definir timestamp principal se solicitado
	if tpp.UseAsLogTime || tpp.TargetField == "timestamp" {
		newEntry.Timestamp = parsedTime
		newEntry.Labels["parsed_timestamp"] = parsedTime.Format(time.RFC3339)
	} else {
		newEntry.Labels[tpp.TargetField] = parsedTime.Format(time.RFC3339)
	}

	return &newEntry, nil
}

// autoDetectTimestamp tenta detectar automaticamente o formato do timestamp
func (tpp *TimestampParseProcessor) autoDetectTimestamp(value string) (time.Time, error) {
	// Regex patterns para diferentes formatos de timestamp
	patterns := []struct {
		regex  string
		format string
	}{
		{`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`, "2006-01-02T15:04:05"},
		{`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`, "2006-01-02 15:04:05"},
		{`^\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}`, "Jan 2 15:04:05"},
		{`^\d{2}/\d{2}/\d{4} \d{2}:\d{2}:\d{2}`, "01/02/2006 15:04:05"},
		{`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}`, "2006/01/02 15:04:05"},
		{`^\d{10}`, "1136214245"}, // Unix timestamp
		{`^\d{13}`, "1136214245000"}, // Unix timestamp milliseconds
	}

	for _, pattern := range patterns {
		matched, _ := regexp.MatchString(pattern.regex, value)
		if matched {
			// Extrair apenas a parte do timestamp
			re := regexp.MustCompile(pattern.regex)
			match := re.FindString(value)
			if match != "" {
				return time.ParseInLocation(pattern.format, match, tpp.TimeZone)
			}
		}
	}

	return time.Time{}, fmt.Errorf("unable to auto-detect timestamp format")
}

// getCommonTimestampFormats retorna formatos comuns de timestamp
func getCommonTimestampFormats() []string {
	return []string{
		// ISO 8601 formats
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",

		// RFC 3339 formats
		time.RFC3339,
		time.RFC3339Nano,

		// Standard log formats
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.000",
		"2006/01/02 15:04:05",
		"01/02/2006 15:04:05",

		// Syslog formats
		"Jan 2 15:04:05",
		"Jan _2 15:04:05",
		"Jan 02 15:04:05",

		// Apache/Nginx formats
		"02/Jan/2006:15:04:05 -0700",
		"02/Jan/2006:15:04:05",

		// Unix timestamps
		"1136214245",     // Unix timestamp
		"1136214245000",  // Unix timestamp milliseconds
	}
}

func (tpp *TimestampParseProcessor) GetType() string {
	return "timestamp_parse"
}

// JSONParseProcessor parseia JSON
type JSONParseProcessor struct {
	Field string
}

func NewJSONParseProcessor(config map[string]interface{}) (*JSONParseProcessor, error) {
	field := "message"
	if f, ok := config["field"].(string); ok {
		field = f
	}

	return &JSONParseProcessor{
		Field: field,
	}, nil
}

func (jpp *JSONParseProcessor) Process(ctx context.Context, entry *types.LogEntry) (*types.LogEntry, error) {
	// Implementação simplificada
	return entry, nil
}

func (jpp *JSONParseProcessor) GetType() string {
	return "json_parse"
}

// FieldAddProcessor adiciona campos
type FieldAddProcessor struct {
	Fields map[string]string
}

func NewFieldAddProcessor(config map[string]interface{}) (*FieldAddProcessor, error) {
	fieldsInterface, ok := config["fields"]
	if !ok {
		return nil, fmt.Errorf("fields is required for field_add")
	}

	// Handle both map[string]interface{} and map[interface{}]interface{} from YAML
	var fieldsMap map[string]interface{}
	switch fields := fieldsInterface.(type) {
	case map[string]interface{}:
		fieldsMap = fields
	case map[interface{}]interface{}:
		fieldsMap = make(map[string]interface{})
		for k, v := range fields {
			if keyStr, ok := k.(string); ok {
				fieldsMap[keyStr] = v
			}
		}
	default:
		return nil, fmt.Errorf("fields must be a map, got %T", fieldsInterface)
	}

	fields := make(map[string]string)
	for k, v := range fieldsMap {
		if strVal, ok := v.(string); ok {
			fields[k] = strVal
		} else {
			// Convert other types to string
			fields[k] = fmt.Sprintf("%v", v)
		}
	}

	return &FieldAddProcessor{
		Fields: fields,
	}, nil
}

func (fap *FieldAddProcessor) Process(ctx context.Context, entry *types.LogEntry) (*types.LogEntry, error) {
	newEntry := *entry
	if newEntry.Labels == nil {
		newEntry.Labels = make(map[string]string)
	}

	for key, value := range fap.Fields {
		newEntry.Labels[key] = value
	}

	return &newEntry, nil
}

func (fap *FieldAddProcessor) GetType() string {
	return "field_add"
}

// FieldRemoveProcessor remove campos
type FieldRemoveProcessor struct {
	Fields []string
}

func NewFieldRemoveProcessor(config map[string]interface{}) (*FieldRemoveProcessor, error) {
	fieldsInterface, ok := config["fields"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("fields is required for field_remove")
	}

	fields := make([]string, len(fieldsInterface))
	for i, field := range fieldsInterface {
		fields[i] = field.(string)
	}

	return &FieldRemoveProcessor{
		Fields: fields,
	}, nil
}

func (frp *FieldRemoveProcessor) Process(ctx context.Context, entry *types.LogEntry) (*types.LogEntry, error) {
	newEntry := *entry
	if newEntry.Labels != nil {
		for _, field := range frp.Fields {
			delete(newEntry.Labels, field)
		}
	}

	return &newEntry, nil
}

func (frp *FieldRemoveProcessor) GetType() string {
	return "field_remove"
}

// LogLevelExtractProcessor extrai nível de log
type LogLevelExtractProcessor struct {
	Pattern *regexp.Regexp
	Field   string
}

func NewLogLevelExtractProcessor(config map[string]interface{}) (*LogLevelExtractProcessor, error) {
	patternStr := `(?i)(debug|info|warn|warning|error|fatal|trace)`
	if p, ok := config["pattern"].(string); ok {
		patternStr = p
	}

	pattern, err := regexp.Compile(patternStr)
	if err != nil {
		return nil, fmt.Errorf("invalid log level pattern: %w", err)
	}

	field := "level"
	if f, ok := config["field"].(string); ok {
		field = f
	}

	return &LogLevelExtractProcessor{
		Pattern: pattern,
		Field:   field,
	}, nil
}

func (llep *LogLevelExtractProcessor) Process(ctx context.Context, entry *types.LogEntry) (*types.LogEntry, error) {
	matches := llep.Pattern.FindStringSubmatch(entry.Message)
	if len(matches) > 1 {
		newEntry := *entry
		if newEntry.Labels == nil {
			newEntry.Labels = make(map[string]string)
		}
		newEntry.Labels[llep.Field] = strings.ToLower(matches[1])
		return &newEntry, nil
	}

	return entry, nil
}

func (llep *LogLevelExtractProcessor) GetType() string {
	return "log_level_extract"
}