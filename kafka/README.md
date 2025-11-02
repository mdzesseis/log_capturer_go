# Kafka Configuration

Este diretório contém as configurações do Apache Kafka para o SSW Logs Capture.

## Arquivos

### `topics-config.yaml`
Definição dos tópicos Kafka com suas configurações:
- **logs-high-priority**: Logs críticos (6 partições)
- **logs-normal-priority**: Logs normais (3 partições)
- **logs-low-priority**: Logs de debug (2 partições)
- **logs**: Tópico genérico (4 partições)
- **logs-dlq**: Dead Letter Queue (2 partições)

### `create-topics.sh`
Script bash para criar os tópicos automaticamente. Execute após o Kafka estar pronto:

```bash
./create-topics.sh
```

Ou via Docker:

```bash
docker exec -it kafka bash -c "cd /etc/kafka/custom && ./create-topics.sh"
```

### `kafka-config.properties`
Configurações customizadas do broker Kafka. Este arquivo é montado no container para sobrescrever configurações padrão.

## Gerenciamento de Tópicos

### Listar Tópicos

```bash
docker exec kafka kafka-topics --bootstrap-server localhost:9092 --list
```

### Descrever um Tópico

```bash
docker exec kafka kafka-topics --bootstrap-server localhost:9092 --describe --topic logs
```

### Produzir Mensagem de Teste

```bash
docker exec -it kafka kafka-console-producer --bootstrap-server localhost:9092 --topic logs
```

### Consumir Mensagens

```bash
docker exec kafka kafka-console-consumer --bootstrap-server localhost:9092 --topic logs --from-beginning
```

### Deletar um Tópico

```bash
docker exec kafka kafka-topics --bootstrap-server localhost:9092 --delete --topic logs-test
```

## Kafka UI

Acesse a interface web do Kafka em: http://localhost:8080

Através dela você pode:
- Visualizar tópicos e mensagens
- Monitorar consumer groups
- Ver métricas do broker
- Gerenciar configurações

## Arquitetura

### Tópicos e Roteamento

O log_capturer_go roteia logs para diferentes tópicos baseado em:

1. **Nível de log**: ERROR/FATAL → high-priority
2. **Tipo de source**: Containers críticos → high-priority
3. **Debug logs**: DEBUG/TRACE → low-priority
4. **Default**: Todos os outros → logs

### Particionamento

Logs são distribuídos por partição usando estratégia de hash baseada em:
- Tenant ID
- Source ID
- Container name

Isso garante que logs da mesma fonte vão para a mesma partição (ordem garantida).

## Performance

### Capacidade por Tópico

- **logs-high-priority**: ~10K logs/sec (6 partições)
- **logs-normal-priority**: ~5K logs/sec (3 partições)
- **logs**: ~7K logs/sec (4 partições)

### Retenção

- **logs-high-priority**: 7 dias, 1GB/partição
- **logs-normal-priority**: 7 dias, 1GB/partição
- **logs-low-priority**: 3 dias, 512MB/partição
- **logs-dlq**: 30 dias, 2GB/partição

## Troubleshooting

### Kafka não inicia

Verifique se o Zookeeper está rodando:

```bash
docker ps | grep zookeeper
docker logs zookeeper
```

### Tópicos não aparecem

Execute o script de criação manualmente:

```bash
docker exec -it kafka bash -c "cd /etc/kafka/custom && ./create-topics.sh"
```

### Performance ruim

Verifique:
1. Número de partições (mais partições = mais paralelismo)
2. Configuração de compressão (snappy é rápido)
3. Batch size do producer (maiores batches = maior throughput)

### Disco cheio

Ajuste retenção em `topics-config.yaml`:

```yaml
retention.ms: 259200000  # 3 dias em vez de 7
retention.bytes: 536870912  # 512MB em vez de 1GB
```

Recrie o tópico após modificar.

## Monitoring

### Verificar Health do Broker

```bash
docker exec kafka kafka-broker-api-versions --bootstrap-server localhost:9092
```

### Verificar Consumer Lag

```bash
docker exec kafka kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group logs-consumer
```

### Métricas Prometheus

O Kafka exporta métricas JMX que podem ser coletadas pelo Prometheus. Veja `prometheus.yml` para configuração.

## Referências

- [Apache Kafka Documentation](https://kafka.apache.org/documentation/)
- [Confluent Platform](https://docs.confluent.io/)
- [Kafka UI](https://github.com/provectus/kafka-ui)
