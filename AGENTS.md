# AGENTS.md - Guia de Agentes e Comandos

Este arquivo serve como um manifesto central para o projeto `log_capturer_go`, detalhando todos os agentes especializados disponíveis, suas responsabilidades e os comandos para interagir com eles.

## 1. Registro Completo de Agentes (24 Total)

Abaixo está a lista completa de todos os agentes especializados disponíveis para este projeto.

```yaml
Agentes Disponíveis (33 Total):

  # Coordenação (1 agente)
  workflow-coordinator: Coordena o workflow, cria issues e integra todos os 23 agentes

  # Desenvolvimento Principal (4 agentes)
  golang: Especialista em desenvolvimento Go
  software-engineering-specialist: SOLID, design patterns, clean code
  architecture: Design de arquitetura de software
  go-bugfixer: Detecção e correção de bugs em Go

  # Testes & Qualidade (3 agentes)
  qa-specialist: Estratégias de teste, automação, quality gates
  continuous-tester: Testes automatizados e validação
  code-reviewer: Qualidade e revisão de código

  # Infraestrutura & Operações (3 agentes)
  infrastructure-specialist: Kubernetes, cloud, IaC (Terraform)
  devops-specialist: Pipelines de CI/CD, automação de deployment
  docker-specialist: Otimização de contêineres e orquestração

  # Monitoramento & Observabilidade (5 agentes)
  observability: Logs, métricas, tracing (geral)
  grafana-specialist: Grafana, Loki, dashboards, visualização
  opentelemetry-specialist: OpenTelemetry SDK, instrumentação, distributed tracing
  trace-specialist: Análise de trace, identificação de gargalos, debugging de performance
  context-analyzer: **(NOVO)** Analisa arquivos (logs, configs) para contexto situacional

  # Tecnologias Especializadas (4 agentes)
  kafka-specialist: Apache Kafka, event streaming
  mysql-specialist: Design de banco de dados, otimização
  opensips-specialist: Configuração OpenSIPS, protocolo SIP
  voip-specialist: RTP, codecs, métricas de qualidade de chamada

  # Monitoramento VoIP (1 agente)
  sip-monitoring-specialist: Monitoramento SIP em tempo real, rastreamento de fluxo de chamada, detecção de fraude

  # Recursos Avançados (1 agente)
  ai-specialist: ML, detecção de anomalias, análise preditiva

  # Documentação & Controle de Versão (2 agentes)
  documentation-specialist: Escrita técnica, docs de API
  git-specialist: Operações de controle de versão

  # Gerenciamento de Componentes (9 agentes)
  task-manager: Gerencia e otimiza a execução de tarefas assíncronas e background jobs
  config-manager: Gerencia configurações dinâmicas e hot-reloads
  resource-manager: Monitora e otimiza o uso de recursos (CPU, memória, disco)
  buffer-manager: Otimiza o uso de buffers de dados e persistência em disco
  position-manager: Gerencia o rastreamento de posições em arquivos e streams
  disk-manager: Gerencia o espaço em disco e políticas de retenção
  anomaly-manager: Detecta anomalias em logs e métricas
  security-manager: Gerencia autenticação, autorização e políticas de segurança
  service-discovery: Gerencia a descoberta e registro de serviços

Tipos de Tarefa & Agentes Responsáveis:

new_feature:
  planning:
    - architecture: Design da arquitetura do sistema
    - software-engineering-specialist: Revisão de design patterns
  implementation:
    - golang: Implementação da funcionalidade principal
    - docker-specialist: Containerização, se necessário
    - opentelemetry-specialist: Adição de instrumentação
  testing:
    - qa-specialist: Criação da estratégia de teste
    - continuous-tester: Execução de testes automatizados
  review:
    - code-reviewer: Revisão da qualidade do código
  deployment:
    - devops-specialist: Configuração do pipeline de CI/CD
    - infrastructure-specialist: Deploy no cluster
  monitoring:
    - observability: Adição de métricas e logs
    - opentelemetry-specialist: Configuração de exportadores de telemetria
    - grafana-specialist: Criação de dashboards
  documentation:
    - documentation-specialist: Escrita de docs técnicos

bug_fix:
  analysis:
    - go-bugfixer: Análise e identificação da causa raiz
    - observability: Verificação de logs e métricas
    - trace-specialist: Análise de traces distribuídos para problemas
    - context-analyzer: Análise de logs brutos e configs para contexto da falha
  implementation:
    - golang: Implementação da correção
  testing:
    - qa-specialist: Verificação da correção
    - continuous-tester: Execução de testes de regressão
  review:
    - code-reviewer: Revisão das mudanças
  deployment:
    - devops-specialist: Deploy de hotfix

performance_optimization:
  analysis:
    - observability: Profiling de gargalos de performance
    - trace-specialist: Análise de caminho crítico e gargalos
    - grafana-specialist: Análise de métricas e tendências
    - context-analyzer: Análise de logs de pprof e dumps de heap
  implementation:
    - golang: Otimização de código
    - software-engineering-specialist: Refatoração de patterns
  validation:
    - qa-specialist: Teste de carga
    - continuous-tester: Benchmarks de performance
    - trace-specialist: Validação de melhorias de performance

database_tasks:
  design:
    - mysql-specialist: Design de schema
    - architecture: Arquitetura de dados
  implementation:
    - golang: Implementação da camada de acesso a dados
  optimization:
    - mysql-specialist: Otimização de queries
  monitoring:
    - observability: Métricas de banco de dados
    - grafana-specialist: Dashboards de banco de dados

infrastructure_tasks:
  provisioning:
    - infrastructure-specialist: Provisionamento de recursos (Terraform)
  containerization:
    - docker-specialist: Otimização de contêineres
  orchestration:
    - infrastructure-specialist: Deploy em Kubernetes
  automation:
    - devops-specialist: Pipelines de CI/CD
  monitoring:
    - observability: Métricas de infraestrutura
    - grafana-specialist: Dashboards de infraestrutura

messaging_tasks:
  kafka_implementation:
    - kafka-specialist: Configuração da infraestrutura Kafka
    - golang: Implementação de producers/consumers
  monitoring:
    - observability: Métricas do Kafka
    - grafana-specialist: Dashboards do Kafka

voip_tasks:
  opensips_config:
    - opensips-specialist: Configuração do OpenSIPS
    - voip-specialist: Ajuste de codecs e qualidade
  monitoring:
    - sip-monitoring-specialist: Monitoramento de chamadas SIP em tempo real
    - voip-specialist: Métricas de qualidade RTP
    - observability: Coleta de métricas VoIP
    - grafana-specialist: Dashboards de qualidade de chamada
  database:
    - mysql-specialist: Otimização do banco de dados de CDR
  fraud_detection:
    - sip-monitoring-specialist: Implementação de padrões de detecção de fraude
  troubleshooting:
    - sip-monitoring-specialist: Análise de fluxo de chamada e diagramas ladder
    - opensips-specialist: Troubleshooting de configuração

observability_tasks:
  opentelemetry_setup:
    - opentelemetry-specialist: Configuração do OTel SDK e coletores
    - golang: Instrumentação do código da aplicação
    - devops-specialist: Deploy de coletores OTel
  trace_analysis:
    - trace-specialist: Análise de traces para issues de performance
    - opentelemetry-specialist: Configuração de estratégias de amostragem
  metrics_implementation:
    - observability: Definição e implementação de métricas
    - opentelemetry-specialist: Configuração de exportadores de métricas OTel
    - grafana-specialist: Criação de visualizações de métricas
  distributed_tracing:
    - opentelemetry-specialist: Configuração da propagação de traces
    - trace-specialist: Análise de dependências de serviço
    - grafana-specialist: Configuração da integração Jaeger/Tempo
  correlation:
    - trace-specialist: Correlação de traces com logs e métricas
    - observability: Implementação de IDs de correlação
    - grafana-specialist: Configuração de dashboards unificados

ai_features:
  implementation:
    - ai-specialist: Modelos de ML e algoritmos
    - golang: Código de integração
  testing:
    - qa-specialist: Validação de modelos
  monitoring:
    - observability: Métricas de modelos de IA
    - grafana-specialist: Dashboards de predição

code_quality:
  review:
    - code-reviewer: Revisão de código
    - software-engineering-specialist: Revisão de arquitetura
  refactoring:
    - golang: Refatoração da implementação
    - go-bugfixer: Correção de issues encontradas
  testing:
    - qa-specialist: Cobertura de teste
    - continuous-tester: Testes automatizados

documentation:
  technical_docs:
    - documentation-specialist: Escrita de docs
    - architecture: Diagramas de arquitetura
  api_docs:
    - documentation-specialist: Especificações OpenAPI
    - golang: Documentação de código
  runbooks:
    - documentation-specialist: Guias operacionais
    - devops-specialist: Procedimentos de deployment
    - infrastructure-specialist: Guias de infraestrutura
    - sip-monitoring-specialist: Guias de troubleshooting de VoIP

security_audit:
  analysis:
    - code-reviewer: Revisão de segurança
    - software-engineering-specialist: Análise de padrões
  implementation:
    - golang: Correções de segurança
    - devops-specialist: Hardening de segurança
  validation:
    - qa-specialist: Testes de segurança

Quando você precisar:
  "Configurar tracing distribuído" → opentelemetry-specialist
  "Analisar gargalos de performance" → trace-specialist
  "Monitorar chamadas SIP" → sip-monitoring-specialist
  "Configurar OpenSIPS" → opensips-specialist
  "Otimizar banco de dados" → mysql-specialist
  "Projetar arquitetura" → architecture
  "Escrever código Go" → golang
  "Corrigir bugs" → go-bugfixer
  "Criar testes" → qa-specialist
  "Revisar código" → code-reviewer
  "Fazer deploy de infraestrutura" → infrastructure-specialist
  "Configurar CI/CD" → devops-specialist
  "Otimizar contêineres" → docker-specialist
  "Criar dashboards" → grafana-specialist
  "Adicionar métricas" → observability
  "Analisar logs para contexto" → context-analyzer  **(NOVO)**
  "Configurar Kafka" → kafka-specialist
  "Analisar qualidade de VoIP" → voip-specialist
  "Implementar ML" → ai-specialist
  "Escrever documentação" → documentation-specialist
  "Gerenciar git" → git-specialist
  "Executar testes automatizados" → continuous-tester
  "Aplicar design patterns" → software-engineering-specialist
  "Gerenciar tarefas assíncronas" → task-manager
  "Gerenciar configurações dinâmicas" → config-manager
  "Monitorar uso de recursos" → resource-manager
  "Otimizar buffers de dados" → buffer-manager
  "Rastrear posições em arquivos" → position-manager
  "Gerenciar espaço em disco" → disk-manager
  "Detectar anomalias" → anomaly-manager
  "Gerenciar segurança" → security-manager
  "Gerenciar descoberta de serviço" → service-discovery