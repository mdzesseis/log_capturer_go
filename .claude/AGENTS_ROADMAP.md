# Agentes - Roadmap de Implementação

## 🤖 Agente Atual
- [x] **golang** - Especialista em desenvolvimento Go

## 📋 Agentes Sugeridos para Implementação

### Prioridade Alta 🔴
1. **devops** - Especialista em DevOps
   - CI/CD pipelines
   - Containerização
   - Orquestração
   - Deploy e releases

2. **docker** - Especialista em Docker
   - Dockerfiles otimizados
   - Docker Compose
   - Multi-stage builds
   - Segurança de containers

3. **observability** - Especialista em Observabilidade
   - Grafana dashboards
   - Prometheus queries
   - Logs e métricas
   - Alertas e SLOs

### Prioridade Média 🟡
4. **architecture** - Especialista em Arquitetura
   - Design patterns
   - Microserviços
   - Escalabilidade
   - Trade-offs arquiteturais

5. **security** - Especialista em Segurança
   - Análise de vulnerabilidades
   - Best practices de segurança
   - OWASP Top 10
   - Sanitização de dados

6. **testing** - Especialista em Testes (geral)
   - Estratégias de teste
   - Test coverage
   - Load testing
   - Chaos engineering

### Prioridade Baixa 🟢
7. **voip** - Especialista em VoIP/Telecom
   - Protocolos SIP
   - OpenSIPS
   - RTP/RTCP
   - Troubleshooting VoIP

8. **database** - Especialista em Bancos de Dados
   - Query optimization
   - Schema design
   - Migrations
   - Performance tuning

## Como Criar um Novo Agente

1. Crie o arquivo em `.claude/agents/<nome>.md`
2. Use o formato YAML frontmatter:
```yaml
---
name: <nome_do_agente>
description: <descrição_breve>
model: sonnet  # ou haiku para tarefas simples
---
```
3. Adicione o prompt especializado do agente
4. Atualize os comandos relevantes para usar o agente

## Exemplo de Estrutura

```markdown
---
name: devops
description: Especialista em práticas DevOps e CI/CD
model: sonnet
---

# DevOps Specialist Agent 🚀

You are a DevOps expert specializing in...

## Core Competencies:
- ...

## Key Responsibilities:
- ...

## Best Practices:
- ...
```

## Benefícios dos Agentes Especializados

1. **Conhecimento Específico**: Cada agente tem expertise profunda em sua área
2. **Respostas Contextualizadas**: Adaptadas ao projeto log_capturer_go
3. **Melhor Performance**: Modelos otimizados para cada tipo de tarefa
4. **Manutenibilidade**: Prompts centralizados e versionados
5. **Consistência**: Padrões uniformes em cada domínio