# Revisão de Código – log_capturer_go (Go 1.24.9)

Data: 2025-11-01
Escopo: /home/mateus/log_capturer_go (código + configs Docker/compose + configs YAML)
Ferramentas usadas: gopls (mcp-gopls, gopls-2), grep estático, leitura dirigida
Resultado dos diagnósticos (amostra): gopls sem erros em arquivos críticos (dispatcher.go, loki_sink.go, local_file_sink.go, initialization.go, config.go, handlers.go)

## Sumário Executivo
- Funcionalidades principais bem estruturadas (dispatcher, monitors, sinks, métricas) e bons cuidados com concorrência (deep copy de LogEntry, semáforo de retries, shutdown ordenado).
- Há desalinhamentos importantes entre arquivo de configuração e o código que fazem recursos configurados não surtirem efeito (CRÍTICO, ver abaixo).
- Segurança e performance em bom nível, mas com pontos de endurecimento (compose roda privilegiado; leitura de diretório para quota em hot path no sink local; compressão por linha em arquivo).

## Erros Críticos (com referências e ação)
1) Dispatcher: campos avançados do YAML são ignorados
- Problema: pkg/types/config.go não modela deduplication/backpressure/degradation/rate_limit em DispatcherConfig, mas internal/dispatcher.DispatcherConfig exige. O app cria o Dispatcher a partir de types.Config, perdendo campos do YAML.
- Evidências:
  - Config YAML: configs/config.yaml:117–167 (deduplication_enabled, deduplication_config, dlq_enabled, backpressure_enabled, degradation_enabled, rate_limit_enabled)
  - Tipos atuais: pkg/types/config.go:98–107 (DispatcherConfig sem esses campos)
  - Uso: internal/app/initialization.go:66–75 monta dispatcher.DispatcherConfig só com básicos; recursos avançados ficam desabilitados
- Impacto: deduplicação, backpressure, degradação e rate limit do dispatcher não funcionam, apesar de configurados.
- Ação: estender pkg/types.DispatcherConfig com os campos avançados (yaml tags idênticas às do YAML) e mapear todos em initialization.go ao construir dispatcher.DispatcherConfig.

2) Local File Sink: rotação e worker_count do YAML não aplicam
- Problema: YAML define local_file.rotation.* e local_file.worker_count, mas types.LocalFileSinkConfig não contém Rotation nem WorkerCount; a conversão em initialization ignora ambos.
- Evidências:
  - YAML: configs/config.yaml:237–247 (rotation.*), 248–249 (worker_count)
  - Tipos: pkg/types/config.go:182–196 (LocalFileSinkConfig sem Rotation/WorkerCount); LocalFileConfig (legacy) possui Rotation/WorkerCount em 386–407
  - Mapeamento: internal/app/initialization.go:117–131 converte Sinks.LocalFile → types.LocalFileConfig sem Rotation/WorkerCount
- Impacto: rotação por tamanho/retention e paralelismo do sink local não são aplicados.
- Ação: adicionar Rotation (RotationConfig) e WorkerCount em types.LocalFileSinkConfig e propagá-los para types.LocalFileConfig no initSinks().

3) Compressão em tempo real por linha no Local File Sink
- Problema: local_file_sink writeEntry comprime cada linha (bytes binários) e grava em .log; compressão deveria ocorrer na rotação (arquivo .gz), não por linha.
- Evidência: internal/sinks/local_file_sink.go:705–721 (uso de compressor por entrada); rotações já oferecem compressão em 596–603.
- Impacto: arquivos .log com payload binário e overhead de CPU por linha; ferramentas padrão de log tornam-se inefetivas.
- Ação: remover compressão por linha (manter apenas compressão na rotação) ou gravar em formato consistente (.gz) com buffering adequado.

4) Duplicidade de verificação de rate limiting no Dispatcher
- Problema: checagem duplicada permite contagem dupla de throttling.
- Evidência: internal/dispatcher/dispatcher.go:552–560 e 562–569 (blocos redundantes)
- Impacto: métricas/erros inflados e complexidade desnecessária.
- Ação: manter apenas um bloco de verificação.

5) getSinkType sempre retorna "unknown"
- Evidência: internal/dispatcher/dispatcher.go:1025–1031
- Impacto: distribuição por sink em stats perde granularidade.
- Ação: identificar tipos conhecidos (LokiSink, LocalFileSink, etc.).

## Gaps de Configuração vs Código (conformidade YAML)
- Dispatcher: deduplication/backpressure/degradation/rate_limit definidos no YAML não mapeados em types.Config (CRÍTICO, ver Erro #1).
- Sinks.local_file.rotation.* e worker_count: presentes no YAML, ausentes em types.LocalFileSinkConfig e não propagados (CRÍTICO, Erro #2).
- Sinks.loki.backpressure_config: existe no YAML; não há estrutura correspondente em types.LokiSinkConfig (opcional, clarificar ou remover do YAML).
- Multi-tenant (multi_tenant.*): presente no YAML (configs/config.yaml:470–578), não há suporte em types.Config nem no app (documentar como “exemplo futuro” ou remover do arquivo principal para evitar confusão).
- Defaults comentados: applyDefaults para loki.push_endpoint/batch_timeout e local_file.filename_pattern (internal/config/config.go:239–248 e 258–263) — avaliar reativação.

## Sugestões Idiomáticas
- Evitar duplicidade de if em rate limit (dispatcher.go:552–569), manter caminho feliz à esquerda.
- Em getSinkType, preferir type switch com nomes curtos e claros; evitar reflection pesada.
- Em funções longas (e.g., processBatch), isolar blocos (anomaly detect, sendToSink) em helpers privados para legibilidade.

## Melhorias de Performance
- Local File Sink: canWriteSize chama getDirSizeGB a cada escrita (internal/sinks/local_file_sink.go:1046–1059) — mover para cache atualizado pelo diskMonitorLoop; manter fast-path só com Statfs.
- Evitar criação/parse de timers excessivos no caminho quente; reutilizar tickers/temporizadores onde possível.
- Loki sink: avaliar limites de transporte HTTP e pooling (já há MaxIdleConns/PerHost em loki_sink.go:91–94; ok). Considerar req.GetBody para robustez de redirects/retries.

## Tratamento de Erros
- Bom uso de fmt.Errorf com %w. Garantir que erros do DLQ no dispatcher e loki sink sejam amostrados (log debug já aplicado em partes do código; manter consistência).
- initialization.go: initSinks retorna erro “no sinks enabled” (138–140) — ok; manter mensagens em minúsculas.

## Segurança
- docker-compose.yml roda privilegiado e como root (lines 11–15): reduzir privilégios (remover privileged/user root), usar usuário appuser e bind mounts mínimos.
- Sanitização de labels para Loki implementada (sanitizeLabelName) — positivo.
- Verificar se endpoints sensíveis do API exigem auth quando security.enabled=true (não avaliado em detalhes; garantir middleware em handlers quando ativado).

## Testes (lacunas e sugestões)
- Adicionar testes de carregamento de configuração que validem mapeamento YAML→types→initialization:
  - Dispatcher campos avançados (dedup/backpressure/degradation/rate_limit)
  - Local file: rotation e worker_count
- Testes de integração “end-to-end” para rotação do sink local (gera arquivo > max_size_mb, valida compressão e retenção).
- Teste de regressão para duplicidade de rate limiting no dispatcher.

## Manutenibilidade e Legibilidade
- Documentar claramente no README que campos enterprise (multi_tenant) são exemplo e não ativos.
- Remover/arquivar pkg/monitoring (marcado como não utilizado) ou integrar com internal/metrics.

## Documentação
- Tipos exportados em pkg/types têm comentários. Completar docs de config avançada após alinhar types com YAML (Dispatcher advanced, LocalFile rotation/worker_count).

## Mapa de Módulos pkg utilizados
- Usados: anomaly, backpressure, batching, buffer, circuit, cleanup, compression, deduplication, degradation, discovery, dlq, docker, errors, goroutines, hotreload, leakdetection, positions, ratelimit, security, selfguard, slo, task_manager, tracing, types, validation.
- Não utilizado: monitoring (candidato a remoção/refatoração).

## Referências Diretas (arquivo:linhas)
- internal/app/initialization.go:66–75, 117–131, 172–220, 296–304
- internal/dispatcher/dispatcher.go:139–168, 552–569, 676–683, 820–944, 946–1021, 1025–1031
- internal/sinks/local_file_sink.go:685–722, 739–750, 540–587, 842–907, 1046–1079
- internal/sinks/loki_sink.go:97–121, 541–573, 598–624
- pkg/types/config.go:98–107, 162–180, 182–196, 376–407, 386–407
- internal/config/config.go:239–248, 258–263
- docker-compose.yml:11–15, 33–47

---

# Conclusão
Principais ações: alinhar types.Config e initialization com o YAML (dispatcher avançado e local_file rotation/worker_count), remover compressão por linha no sink local, e corrigir duplicidade de rate limit. Ver Plano de Ação detalhado em relatorios_code_review/PLANO_ACAO_2025-11-01.md.
