# Instruções de analise de codigo e projeto — log_capturer_go

 **versão do Go:** 1.24.9

Você é um engenheiro de software sênior com vasta experiência em Go (Golang). Por favor, revise o código Go fornecido
    abaixo. Seu objetivo é garantir a qualidade, eficiência, segurança e conformidade com as melhores práticas da 
   comunidade Go. Utilize os subagentes disponiveis para auxiliar.

**Foco da Revisão:** Principal e unico ponto revise todos os arquivos de codigo go e encontre e a valide se realmente é um resource leak, leaks buscados filedescriptors leak memory leak e goroutines leak, não precisa revisar aruqivos de teste, documente e evidencie poruqe ali é leak e como resolver 

Objetivo
- Orientar um especialista em Go e DevOps a validar e evoluir o log_capturer_go para um nível enterprise: alta confiabilidade, alto desempenho, sem vazamentos de recursos, sem duplicidade de logs, com backpressure, DLQ, observabilidade forte e operação segura em contêiner.
- Cenário-alvo: coleta de logs de sistema e serviços VoIP (OpenSIPS) e banco (MySQL), rodando no mesmo host (ou stack docker-compose) sem impactar a saúde dos serviços.

Escopo do review
- Cobrir arquitetura Go, configuração e validação, pipelines de processamento, dispatcher/sinks, monitores (arquivos/containers), posições/rotação, backpressure/dlq/deduplicação, persistência (disk buffer), observabilidade, performance e tuning, segurança, empacotamento Docker, operação no host junto a OpenSIPS/MySQL, testes e CI/CD.

Referências do projeto (usar ativamente)
- Entrada/fluxo de inicialização: cmd/main.go → internal/app (New/Run/Start/Stop, initializeComponents, registerHandlers)
- Config principal: configs/config.yaml; pipelines: configs/pipelines.yaml; roteamento por arquivo: configs/file_pipeline.yml
- Endpoints (porta 8401): /health, /stats, /config, /config/reload, /positions, /dlq/stats, /dlq/reprocess, /metrics (proxy :8001)
- Componentes-chave: internal/dispatcher, internal/sinks, file/container monitors, pkg/dlq, pkg/deduplication, pkg/backpressure
- Gotchas: manter ordem de init; /metrics é proxy; defaults vs empty para files_config; config.ValidateConfig obrigatória

Critérios de aprovação (níveis)
- Nível 1 — Pronto p/ Produção controlada: sem vazamentos, sem pânico, at-least-once com deduplicação eficaz, backpressure + DLQ funcionais, SLOs e métricas/alertas básicas, container hardened, desempenho estável >= 10k LPS com picos sem perda.
- Nível 2 — Enterprise: tudo do N1 + testes de carga/soak/caos, rotinas de recuperação robustas, upgrades sem perda, documentação operacional completa, alertas abrangentes, throughput sustentado conforme metas (ex.: 25k–50k LPS) com overhead baixo no host OpenSIPS/MySQL.

Checklist técnico detalhado

1) Arquitetura Go e ciclo de vida
- Confirmar ordem de inicialização: Dispatcher → Sinks → Positions → Monitors → Aux → Hot reload/Discovery → HTTP+Metrics.
- Garantir Start/Stop idempotentes e gracioso: cancel context, parar HTTP/monitores/aux, drenar dispatcher, parar sinks.
- Verificar timeouts e shutdown deadlines; evitar goroutine leaks (use WaitGroup; se go >=1.25, considerar WaitGroup.Go).
- Conferir logs de inicialização claros, incluindo versão, config efetiva resumida e validações.

2) Configuração e overrides
- internal/config/config.go: validar que defaults são aplicados corretamente e overrides via env (SSW_CONFIG_FILE, SSW_PIPELINES_FILE, SSW_FILE_CONFIG, LOKI_URL, LOG_LEVEL, LOG_FORMAT) funcionam.
- Hot reload: validar schema, validação antes de aplicar, troca atômica sem race e sem perda/duplicidade.
- Semântica defaults vs slice vazio (files_config.*) preservada.

3) Processamento e pipelines
- Garantir que configs/pipelines.yaml e file_pipeline.yml estejam sincronizados com a lógica de enriquecimento, labels e roteamento.
- Validar tratamento de multiline (SIP trace, stack traces), fuso horário, time parsing, non-UTF-8, truncamento de linhas muito grandes (limites configuráveis).
- Evitar explosão de cardinalidade em labels (Loki): normalizar/limitar campos dinâmicos.

4) Dispatcher e Sinks (Loki/arquivo local)
- Dispatcher: checar tamanho do batch, concorrência, retries com backoff exponencial, jitter, limites de fila, circuit breaking.
- Sem perda em erro transitório: at-least-once com reenvio; definir limites de retry antes de DLQ.
- Sinks: Loki client com timeouts, compressão, reuso de conexões, cálculo de GetBody p/ retries; flush garantido no shutdown.
- Garantir ordenação por stream quando aplicável; avaliar paralelismo seguro por labels.

5) Monitores: arquivos e contêineres
- Arquivos: usar inotify; tratar rotação (rename/copytruncate), truncamento, reabertura por inode; seguir positions.
- Containers: reconectar robusto a docker.sock; filtros por labels/names; lidar com linhas parciais e timestamps do Docker.
- Sistema: planejar coleta de /var/log/* e journald (se adotado) com mínimos privilégios.

6) Posições e rotação
- Persistência atômica e fsync periódico, proteção contra corrupção; flush no shutdown.
- Restauração fiel no startup; evitar duplicidade em rotações rápidas; TTL/compactação se necessário.

7) Backpressure, DLQ e Deduplicação
- Backpressure: saturação deve reduzir ritmo de leitura/envio sem drop; expor métricas de fila, latência e rejeições.
- DLQ: formato, retenção, limites, reprocessamento via /dlq/reprocess com idempotência e segurança.
- Deduplicação: chave estável (arquivo: path+inode+offset+hash; container: containerID+timestamp+nano+hash). Janela temporal e tamanho configuráveis.

8) Persistência: disk buffer
- Dimensão e limites; política de limpeza; fsync e compactação; comportamento em falta de espaço.
- Testar crash/restart no meio de backpressure sem perda.

9) Observabilidade
- Métricas Prometheus: throughput por estágio, latências, filas, retries, failed sends, dedup hits, DLQ enqueues, consumo de CPU/mem/FDs, goroutines.
- /metrics proxy em 8401 para servidor 8001 deve permanecer funcional.
- Tracing opcional; logs da própria aplicação estruturados e com correlação.
- Dashboards e alertas (Grafana/Prometheus):
  - Taxa de perdas (deve ser zero), crescimento de DLQ, saturação de filas, erro de sinks, tempo de flush.

10) Performance e tuning
- Metas: baixa latência p99 sob 50–150ms p/ envio, throughput sustentado (10k–50k LPS conforme meta), footprint de CPU/RAM contido.
- Batch size e concorrência calibráveis; uso de buffers e pooling para reduzir alocações; evitar cópias desnecessárias.
- GC/Memory: considerar GOGC/GOMEMLIMIT em container; monitorar alocações por registro.
- I/O: controlar fsync, write coalescing; evitar lock contention; evitar conversões de string/[]byte supérfluas.

11) Confiabilidade (sem perda/duplicidade)
- Provar at-least-once end-to-end com dedup eficaz; cenários: rotação, quedas de Loki/rede, restarts, picos.
- Testar idempotência de reprocessamento DLQ; evitar duplicatas após hot reload.

12) Segurança (App e Container)
- Rodar como usuário não-root, root filesystem read-only, no-new-privileges, capabilities mínimas.
- Docker socket: se usado, limitar acesso e escopo; preferir read-only.
- Secrets/URLs (ex.: LOKI_URL com auth): não logar; montar via env/secret; TLS verificado.
- Dependências auditadas (govulncheck, gosec) e imagens base pinadas; SBOM e assinaturas quando possível.

13) Empacotamento Docker e execução
- Dockerfile multi-stage com build estático (CGO_ENABLED=0); imagem final distroless/static ou scratch; HEALTHCHECK.
- Variáveis de ambiente padrão sensatas; logs STDOUT/STDERR da app; montagem de volumes /var/log, pipelines/configs.
- docker-compose: limites de CPU/mem, reinício, depends_on/healthcheck; portas 8401/8001/3100/3000/9090 conforme stack.

14) Convivência com OpenSIPS/MySQL no mesmo host
- Isolamento de recursos: cgroup CPU/mem/io; nice/ionice; limites de FDs.
- inotify limits ajustados; evitar varreduras agressivas; definir include_patterns explícitos para diretórios de alto churn.
- Não competir por disco: writeback controlado, buffers dimensionados.

15) Especificidades OpenSIPS/MySQL
- OpenSIPS: formatos de logs (transaction IDs, call-ids), multiline SIP, alta taxa em burst; confirmar parsing e labels úteis.
- MySQL: slow query logs, error logs; rotação copytruncate comum; garantir reabertura correta.
- Garantir que pipelines e roteamento (file_pipeline.yml) segreguem streams por serviço e severidade.

16) Robustez e casos extremos
- Linhas enormes, binários, non-UTF8, timestamps ausentes ou fora de ordem, time-skew e NTP.
- Falta de espaço em disco; perda de conectividade Loki; falhas de DNS; relógio mudando.
- Múltiplas instâncias acidentais no host: evitar dupla coleta com exclusão de diretórios/lock/alerta operacional.

17) Testes e validação
- go test por pacote afetado; testes de carga (tests/load) com metas definidas (10k/25k/50k/100k) e sustentado.
- Soak > 24h sem leaks (goroutines, FDs, memória) e sem perdas.
- Caos: derrubar Loki, simular rotação rápida, matar processo durante flush; validar recuperação sem perda/duplicidade.

18) CI/CD e qualidade
- Pipelines: build, testes, lint (staticcheck/golangci-lint se aplicável), govulncheck, gosec, docker build+scan, SBOM.
- go mod tidy consistente; versões pinadas; reproducible builds.
- Regras de branch/PR, revisão obrigatória e checks bloqueantes.

19) Documentação e operação
- README atualizado com execução local (go run) e docker-compose; variáveis de ambiente; hot reload.
- Runbooks: troubleshooting (fila cheia, DLQ, Loki down), capacidades/limites e procedimentos de upgrade sem perda.
- Políticas de retenção DLQ/disk buffer; planos de capacity planning.

Procedimento do reviewer
1) Ler README, configs padrão, e endpoints; validar health e métricas rodando localmente.
2) Auditar inicialização/stop, config.ValidateConfig, e caminhos quentes do dispatcher e sinks.
3) Exercitar cenários de rotação e picos; observar métricas, DLQ e backpressure.
4) Rodar testes de carga e soak; revisar consumo de recursos e vazamentos.
5) Avaliar Dockerfile e execução endurecida; validar segurança e menor privilégio.
6) Emitir relatório com gaps, severidade, e plano de ação para atingir Nível 2.

Definição de pronto (DoD) Enterprise
- Zero vazamentos em 24h de soak; perda de logs = 0 em cenários cobertos; duplicidade < 0,001% com dedup ativo.
- Backpressure/DLQ funcionando e observável; alertas configurados; container hardened; throughput meta atingida.
- Documentação e runbooks completos; CI/CD com verificações de segurança e qualidade.

Apêndice — Comandos úteis
- Local: go run cmd/main.go --config configs/config.yaml
- Stack: docker-compose up -d; curl http://localhost:8401/health; curl http://localhost:8001/metrics
- Testes: go test ./... (ou por pacote alterado); cargas em tests/load conforme README.


   **Servidores mcp utilizados no projeto:**
   1. mcp-gopls
   2. gopls-2

   **Ferramentas utilizadas na revisão:**
   1. Utilize todas as as ferramentas MCP disponíveis para análise estática de código, detecção de vulnerabilidades e 
   verificação de conformidade com as melhores práticas de Go.
   2. Utilize demais ferramentas disponiveis para garantir a qualidade e funcionabilidade do código.
   3. Uber Go Leak Detector (go.uber.org/goleak) 
   4. runtime.NumGoroutine()


# Guia de Estilo de Codificação Go

Essas regras são projetadas para orientar a geração de código Go que seja simples, legível e fácil de manter, aderindo ao estilo idiomático do Go e aos princípios da engenharia pragmática.

## 1. O Princípio da Menor Abstração

Seu objetivo principal é clareza, não esperteza. Comece com a solução mais simples possível.

- **Regra 1.1: Padrão para uma Única Função** - Resolva o problema dentro de uma única função primeiro. Não crie funções auxiliares, novos tipos ou novos pacotes prematuramente.

- **Regra 1.2: Justifique Cada Abstração** - Antes de criar uma nova função, struct ou pacote, você deve justificar sua existência com base nas regras abaixo (por exemplo, comprimento da função, contagem de parâmetros ou a Regra dos Três). Se não houver uma boa razão para abstrair, não o faça.

## 2. Design e Granularidade da Função

Funções são os blocos de construção fundamentais. Elas devem ser claras e focadas.

- **Regra 2.1: Funções Fazem Uma Coisa** - Cada função deve ter uma única responsabilidade clara. Se você não consegue descrever o que uma função faz em uma frase simples, ela está fazendo demais.

- **Regra 2.2: Limite Estrito de Comprimento da Função** - Uma função raramente deve exceder 50 linhas. Se uma função crescer, decomponha-a imediatamente em funções auxiliares privadas menores. Mantenha esses auxiliares no mesmo arquivo para manter a localidade.

- **Regra 2.3: Limite Estrito de Parâmetros** - Uma função não deve ter mais de quatro parâmetros.
    - Se você precisar de mais, agrupe os parâmetros relacionados em uma struct.
    - Se uma função precisar operar em um estado compartilhado, torne-a um método em uma struct que contém esse estado. Isso é preferível a passar o estado por meio de vários parâmetros de função.

- **Regra 2.4: Valores de Retorno** - Retorne um ou dois valores diretamente. Se você precisar retornar três ou mais valores relacionados, use uma struct nomeada para dar contexto e clareza a eles. Evite retornar um mapa ou uma tupla simples de muitos valores.

## 3. Duplicação vs. Abstração

Evite abstrações apressadas. A duplicação é frequentemente melhor do que a abstração errada.

- **Regra 3.1: A Regra dos Três** - Não refatore código duplicado em sua primeira ou segunda aparição. Somente quando você encontrar a terceira instância, você deve considerar a criação de uma abstração compartilhada (como uma nova função).

- **Regra 3.2: Verifique a Verdadeira Duplicação** - Antes de refatorar, confirme se o código duplicado representa a mesma lógica principal. Se os blocos de código parecerem semelhantes por coincidência, mas lidarem com regras de negócios diferentes que podem mudar independentemente, eles devem permanecer separados. Criar uma abstração aqui criaria uma dependência fortemente acoplada, mas logicamente não relacionada.

## 4. Filosofia de Pacotes e Interfaces

Siga a abordagem idiomática do Go para pacotes e interfaces.

- **Regra 4.1: Pacotes Têm um Propósito Singular** - Um pacote deve representar um único conceito (por exemplo, `http`, `user`, `models`). Não crie pacotes genéricos "utilitários", "comuns" ou "auxiliares". Mantenha tipos e funções relacionados juntos em um pacote coeso.

- **Regra 4.2: Interfaces são Definidas pelo Consumidor** - Não defina interfaces grandes e monolíticas no lado do produtor. Em vez disso, a função que usa uma dependência deve definir uma pequena interface descrevendo apenas o comportamento que ela requer. Isso segue o provérbio Go: "Quanto maior a interface, mais fraca a abstração."

- **Regra 4.3: Mantenha as Interfaces Pequenas** - Uma interface deve idealmente ter apenas um método. Interfaces com mais de três métodos são um sinal de alerta e devem ser reavaliadas.
