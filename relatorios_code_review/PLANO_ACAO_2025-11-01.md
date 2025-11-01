# Plano de Ação – Correções e Melhorias
Data: 2025-11-01

Objetivo: aplicar as correções mínimas necessárias para alinhar o YAML com o código e eliminar problemas críticos, mantendo comportamento existente.

## 1) Dispatcher – habilitar recursos avançados (dedup/backpressure/degradation/rate_limit)
- Editar: pkg/types/config.go
  - Estender type DispatcherConfig (yaml:"dispatcher") adicionando campos:
    - DeduplicationEnabled bool `yaml:"deduplication_enabled"`
    - DeduplicationConfig  deduplication.Config `yaml:"deduplication_config"`
    - DLQEnabled bool `yaml:"dlq_enabled"` (já existe em internal, manter aqui também)
    - BackpressureEnabled bool `yaml:"backpressure_enabled"`
    - BackpressureConfig  backpressure.Config `yaml:"backpressure_config"`
    - DegradationEnabled bool `yaml:"degradation_enabled"`
    - DegradationConfig  degradation.Config `yaml:"degradation_config"`
    - RateLimitEnabled bool `yaml:"rate_limit_enabled"`
    - RateLimitConfig  ratelimit.Config `yaml:"rate_limit_config"`
- Editar: internal/app/initialization.go
  - Na montagem de dispatcherConfig (linhas ~66–75), popular os novos campos a partir de app.config.Dispatcher.
- Aceite: gopls sem erros; NewDispatcher passa a inicializar managers quando enabled via YAML; testes existentes do dispatcher continuam passando.

## 2) Local File Sink – propagar rotation e worker_count
- Editar: pkg/types/config.go
  - Adicionar em LocalFileSinkConfig:
    - WorkerCount int `yaml:"worker_count"`
    - Rotation RotationConfig `yaml:"rotation"`
- Editar: internal/app/initialization.go
  - Na conversão para types.LocalFileConfig (linhas ~117–131), incluir:
    - WorkerCount: app.config.Sinks.LocalFile.WorkerCount
    - Rotation:    app.config.Sinks.LocalFile.Rotation
- Aceite: rotação por tamanho/retention e quantidade de workers passam a respeitar o YAML; validar manualmente criando arquivo > max_size_mb.

## 3) Local File Sink – remover compressão por linha
- Editar: internal/sinks/local_file_sink.go
  - Em writeEntry (685–722): remover bloco que comprime cada linha (lf.useCompression/lf.compressor) e gravar sempre texto/JSON puro.
  - Manter compressão apenas na rotação (compressFile já cobre .gz).
- Aceite: arquivos .log permanecem texto/JSON; arquivos rotacionados .gz; queda de CPU por escrita; testes de leitura de logs funcionam.

## 4) Dispatcher – consolidar checagem de rate limit
- Editar: internal/dispatcher/dispatcher.go
  - Em Handle (552–569): remover um dos blocos duplicados; manter apenas o que respeita d.config.RateLimitEnabled.
- Aceite: métricas de Throttled não duplicam; sem alteração de semântica.

## 5) Dispatcher – identificar tipo de sink
- Editar: internal/dispatcher/dispatcher.go:getSinkType
  - Implementar type switch para retornar "loki", "local_file", "elasticsearch", "splunk" quando aplicável.
- Aceite: stats.SinkDistribution passa a refletir tipos reais.

## 6) Defaults e limpeza de YAML
- Opcional: reativar defaults comentados em internal/config/config.go (239–248, 258–263) se desejado.
- Documentar no README que seções enterprise (multi_tenant.*) são exemplos; mover para um arquivo "enterprise-config.yaml" (já existe) e remover do config.yaml padrão.

## 7) Performance – quota do diretório fora do hot path
- Editar: internal/sinks/local_file_sink.go
  - Em canWriteSize (1046–1079): substituir cálculo síncrono do diretório por cache atomizado atualizado por diskMonitorLoop; no hot path usar apenas valores de cache + Statfs rápido.
- Aceite: menos I/O por escrita; sem regressão funcional.

## 8) Segurança – docker-compose
- Editar: docker-compose.yml
  - Remover privileged: true e user: "0:0"; usar usuário "1000:1000" (appuser) e volumes somente leitura quando possível.
- Aceite: stack sobe e coleta logs; menor superfície de ataque.

## 9) Testes
- Adicionar testes (tests/config):
  - Carregamento de configs validando mapeamento de Dispatcher avançado e LocalFile rotation/worker_count.
- Adicionar teste de integração: rotação do Local File Sink.

## Sequência Recomendada
1. Itens 1 e 2 (mapeamento YAML→types→init) – CRÍTICOS.
2. Itens 3 e 4 (correções rápidas de comportamento).
3. Item 5 (observabilidade), 6 (higiene), 7 (performance), 8 (segurança), 9 (testes).

## Riscos e Mitigações
- Mudanças em types/config.go exigem atenção a compatibilidade YAML: manter nomes e tipos idênticos aos já usados no config.yaml.
- Ajustes no Local File Sink devem preservar defaults (fila/worker). Validar em ambiente de teste com carga.

## Checklist de Aceite
- [ ] Dedup/backpressure/degradation/rate_limit funcionando quando setados no YAML
- [ ] Rotação e worker_count do sink local respeitados
- [ ] Arquivos .log em texto/JSON; .gz somente após rotação
- [ ] Throttling sem contagem dupla
- [ ] SinkDistribution por tipo correta
- [ ] Compose sem root/privileged
- [ ] Testes de configuração e rotação passando
