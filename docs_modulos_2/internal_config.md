# Módulo Config (internal/config)

## Estrutura e Responsabilidades

O módulo `config` é responsável pelo **carregamento, validação e gerenciamento** de toda a configuração da aplicação. Este módulo fornece uma interface centralizada para acessar configurações vindas de arquivos YAML, variáveis de ambiente e defaults do sistema.

### Arquivos Principais:
- `config.go` - Lógica principal de carregamento e validação de configuração

## Funcionamento

### Processo de Carregamento:
1. **Carregamento de Arquivo** (`loadConfigFile()`)
   - Lê arquivo YAML especificado
   - Faz parse da estrutura de configuração
   - Valida sintaxe YAML

2. **Aplicação de Defaults** (`applyDefaults()`)
   - Define valores padrão para todos os campos
   - Garante configuração mínima funcional
   - Estabelece valores seguros

3. **Override por Variáveis de Ambiente** (`applyEnvironmentOverrides()`) ✅
   - Permite override de qualquer configuração via env vars
   - Suporte a tipos complexos (maps, slices, durations)
   - Prefixo padrão `SSW_` para todas as variáveis

4. **Carregamento de Pipeline de Arquivos** (`loadFilePipeline()`)
   - Carrega configuração específica de monitoramento de arquivos
   - Suporte a pipeline files externos
   - Configuração dinâmica de paths

### Estrutura de Configuração:
```go
type Config struct {
    // Core Settings
    App                 AppConfig
    Server              ServerConfig
    Metrics             MetricsConfig

    // Processing Pipeline
    Processing          ProcessingConfig
    Dispatcher          DispatcherConfig

    // Input Sources
    FileMonitorService  FileMonitorServiceConfig
    ContainerMonitor    ContainerMonitorConfig

    // Output Destinations
    Sinks               SinksConfig

    // Storage & Persistence
    Positions           PositionsConfig
    DiskBuffer          DiskBufferConfig
    DiskCleanup         DiskCleanupConfig

    // Enterprise Features
    Security            SecurityConfig
    Tracing             TracingConfig
    SLO                 SLOConfig
    ServiceDiscovery    ServiceDiscoveryConfig
    HotReload           HotReloadConfig
}
```

## Papel e Importância

### Centralização de Configuração:
- **Single Source of Truth**: Ponto único para toda configuração da aplicação
- **Consistency**: Garante consistência entre componentes
- **Validation**: Validação centralizada de configurações

### Flexibilidade Operacional:
- **Environment Overrides**: Permite customização sem alterar arquivos ✅
- **Hot Reload**: Capacidade de recarregar configuração em runtime
- **Default Values**: Valores sensatos que funcionam out-of-the-box

### Segurança:
- **Sanitization**: Remove informações sensíveis ao expor configuração
- **Validation**: Previne configurações inseguras ou inválidas
- **Environment Separation**: Configurações específicas por ambiente

## Configurações Aplicáveis

### Configuração Base da Aplicação:
```yaml
app:
  name: "ssw-logs-capture"
  version: "v1.0.0"
  environment: "production"  # development, staging, production
  log_level: "info"          # debug, info, warn, error
  log_format: "json"         # json, text
```

### Servidor HTTP:
```yaml
server:
  enabled: true
  host: "0.0.0.0"
  port: 8080
  read_timeout: "30s"
  write_timeout: "30s"
  idle_timeout: "60s"
```

### Métricas Prometheus:
```yaml
metrics:
  enabled: true
  port: 8090
  path: "/metrics"
  namespace: "ssw_logs_capture"
```

### Dispatcher (Orquestrador):
```yaml
dispatcher:
  queue_size: 10000
  worker_count: 4
  batch_size: 100
  batch_timeout: "5s"
  max_retries: 3
```

### Sinks (Destinos):
```yaml
sinks:
  loki:
    enabled: true
    url: "http://localhost:3100"
    batch_size: 100
    batch_timeout: "10s"
    compression: true
    auth:
      type: "basic"  # basic, bearer
      username: "user"
      password: "pass"

  local_file:
    enabled: true
    directory: "/var/log/captured"
    output_format: "json"  # json, text
    rotation:
      max_size_mb: 100
      max_files: 10
      compress: true
```

### Monitoramento de Contêineres:
```yaml
container_monitor:
  enabled: true
  socket_path: "/var/run/docker.sock"
  include_labels:
    environment: "production"
  exclude_labels:
    internal: "true"
```

### Recursos Enterprise:
```yaml
security:
  enabled: false
  authentication:
    enabled: true
    method: "jwt"
    session_timeout: "24h"

service_discovery:
  enabled: false
  update_interval: "30s"
  docker_enabled: true

hot_reload:
  enabled: false
  check_interval: "10s"
  watch_files:
    - "/etc/config.yaml"
```

## Variáveis de Ambiente Suportadas ✅

### Aplicação:
- `SSW_APP_NAME` - Nome da aplicação
- `SSW_APP_VERSION` - Versão da aplicação
- `SSW_APP_ENVIRONMENT` - Ambiente (dev/staging/prod)
- `SSW_LOG_LEVEL` - Nível de log
- `SSW_LOG_FORMAT` - Formato de log

### Servidor:
- `SSW_SERVER_ENABLED` - Habilitar servidor HTTP
- `SSW_SERVER_HOST` - Host do servidor
- `SSW_SERVER_PORT` - Porta do servidor

### Métricas:
- `SSW_METRICS_ENABLED` - Habilitar métricas
- `SSW_METRICS_PORT` - Porta das métricas
- `SSW_METRICS_PATH` - Path das métricas

### Loki:
- `SSW_LOKI_ENABLED` - Habilitar sink Loki
- `SSW_LOKI_URL` - URL do Loki
- `SSW_LOKI_USER` - Usuário para autenticação
- `SSW_LOKI_PASSWORD` - Senha para autenticação
- `SSW_LOKI_TOKEN` - Token bearer para autenticação
- `SSW_LOKI_HEADERS` - Headers customizados (formato: KEY1=value1,KEY2=value2)
- `SSW_LOKI_LABELS` - Labels padrão (formato: KEY1=value1,KEY2=value2)

### Monitoramento:
- `SSW_FILE_MONITOR_ENABLED` - Habilitar monitoramento de arquivos
- `SSW_CONTAINER_MONITOR_ENABLED` - Habilitar monitoramento de contêineres
- `SSW_DOCKER_SOCKET` - Path do socket Docker

## Funções Utilitárias

### Conversão de Tipos:
```go
func getEnvString(key, defaultValue string) string
func getEnvInt(key string, defaultValue int) int
func getEnvBool(key string, defaultValue bool) bool
func getEnvDuration(key string, defaultValue time.Duration) time.Duration
func getEnvStringSlice(key string, defaultValue []string) []string
func getEnvStringMap(key string, defaultValue map[string]string) map[string]string
```

### Validação:
```go
type ConfigValidator struct {
    config *Config
    errors []error
}

func (v *ConfigValidator) validateRequired()
func (v *ConfigValidator) validateSinks()
func (v *ConfigValidator) validateMonitors()
func (v *ConfigValidator) validateNetworking()
```

## Problemas Conhecidos

### Validação:
- **Incomplete Validation**: Nem todos os cenários são validados
- **Cross-Field Validation**: Dependências entre campos não são verificadas
- **Runtime Validation**: Algumas validações só acontecem em runtime

### Performance:
- **Large Configs**: Configurações muito grandes podem ser lentas para carregar
- **File Watching**: Watch de arquivos pode consumir muitos file descriptors
- **Environment Parsing**: Parsing de maps complexos via env vars é limitado

### Segurança:
- **Secret Exposure**: Configurações podem expor segredos em logs
- **File Permissions**: Arquivos de configuração podem ter permissões incorretas
- **Environment Leakage**: Variáveis de ambiente podem vazar entre processos

## Melhorias Propostas

### Validação Avançada:
```go
type ConfigSchema struct {
    Fields map[string]FieldSchema
    Rules  []ValidationRule
}

type FieldSchema struct {
    Type        reflect.Type
    Required    bool
    Default     interface{}
    Validation  []Validator
}

type ValidationRule struct {
    Fields    []string
    Condition func(config *Config) bool
    Message   string
}
```

### Configuration Templates:
```go
type ConfigTemplate struct {
    Name        string
    Environment string
    Overrides   map[string]interface{}
}

func LoadTemplate(name, env string) (*ConfigTemplate, error)
func (t *ConfigTemplate) Apply(config *Config) error
```

### Secrets Management:
```go
type SecretResolver interface {
    Resolve(secretRef string) (string, error)
}

type VaultSecretResolver struct {
    client *vault.Client
}

type EnvSecretResolver struct{}
type FileSecretResolver struct{}
```

### Configuration Encryption:
```go
type ConfigEncryption struct {
    key    []byte
    cipher cipher.AEAD
}

func (e *ConfigEncryption) EncryptField(value string) string
func (e *ConfigEncryption) DecryptField(encrypted string) string
```

### Hot Reload Avançado:
```go
type ConfigWatcher struct {
    fsnotify.Watcher
    callbacks []ConfigCallback
    debouncer *time.Timer
}

type ConfigCallback func(old, new *Config) error
```

### Configuration Versioning:
```go
type ConfigVersion struct {
    Version   string
    Timestamp time.Time
    Changes   []ConfigChange
}

type ConfigChange struct {
    Field string
    Old   interface{}
    New   interface{}
}
```

## Exemplo de Uso

### Carregamento Básico:
```go
config, err := config.LoadConfig("/path/to/config.yaml")
if err != nil {
    log.Fatalf("Failed to load config: %v", err)
}

if err := config.ValidateConfig(config); err != nil {
    log.Fatalf("Invalid config: %v", err)
}
```

### Override com Environment Variables:
```bash
export SSW_LOKI_URL="http://production-loki:3100"
export SSW_LOG_LEVEL="debug"
export SSW_LOKI_HEADERS="X-Team=platform,X-Service=logs"

./log_capturer --config=production.yaml
```

### Validação Customizada:
```go
validator := config.NewConfigValidator(cfg)
if err := validator.Validate(); err != nil {
    log.Fatalf("Configuration validation failed: %v", err)
}
```

## Dependências

### Bibliotecas Externas:
- `gopkg.in/yaml.v2` - Parsing de YAML
- `github.com/sirupsen/logrus` - Logging estruturado

### Módulos Internos:
- `pkg/types` - Definições de tipos de configuração
- `pkg/errors` - Sistema de erros estruturados

## Arquivos de Configuração Suportados

### Configuração Principal:
- `config.yaml` - Configuração principal da aplicação
- `config.yml` - Alternativa para a configuração principal

### Configurações Específicas:
- `file_pipeline.yml` - Configuração específica de monitoramento de arquivos
- `security.yaml` - Configurações avançadas de segurança
- `tracing.yaml` - Configurações de distributed tracing

O módulo `config` é **fundamental** para a operação de toda a aplicação, fornecendo flexibilidade, segurança e facilidade de operação através de uma interface de configuração robusta e bem estruturada.