#!/bin/bash
# =============================================================================
# KAFKA TOPICS CREATION SCRIPT
# =============================================================================
# Script para criar tópicos Kafka automaticamente
# Uso: ./create-topics.sh
# =============================================================================

set -e

KAFKA_BROKER="${KAFKA_BROKER:-kafka:9092}"
ZOOKEEPER="${ZOOKEEPER:-zookeeper:2181}"

echo "========================================="
echo "Kafka Topics Creation Script"
echo "========================================="
echo "Kafka Broker: $KAFKA_BROKER"
echo "Zookeeper: $ZOOKEEPER"
echo "========================================="

# Função para criar tópico
create_topic() {
    local topic_name=$1
    local partitions=$2
    local replication=$3
    local configs=$4

    echo ""
    echo "Creating topic: $topic_name"
    echo "  Partitions: $partitions"
    echo "  Replication Factor: $replication"

    # Verifica se o tópico já existe
    if kafka-topics --bootstrap-server $KAFKA_BROKER --list | grep -q "^${topic_name}$"; then
        echo "  Status: Already exists ✓"
        return 0
    fi

    # Cria o tópico
    kafka-topics --bootstrap-server $KAFKA_BROKER \
        --create \
        --topic $topic_name \
        --partitions $partitions \
        --replication-factor $replication \
        --config $configs

    if [ $? -eq 0 ]; then
        echo "  Status: Created successfully ✓"
    else
        echo "  Status: Failed to create ✗"
        return 1
    fi
}

# Aguarda Kafka estar disponível
echo ""
echo "Waiting for Kafka to be ready..."
until kafka-broker-api-versions --bootstrap-server $KAFKA_BROKER > /dev/null 2>&1; do
    echo "  Kafka not ready yet, waiting..."
    sleep 5
done
echo "  Kafka is ready ✓"

echo ""
echo "========================================="
echo "Creating Topics..."
echo "========================================="

# Tópico de alta prioridade
create_topic "logs-high-priority" 6 1 \
    "retention.ms=604800000,retention.bytes=1073741824,segment.bytes=536870912,compression.type=snappy,cleanup.policy=delete,min.insync.replicas=1,max.message.bytes=1048576"

# Tópico de prioridade normal
create_topic "logs-normal-priority" 3 1 \
    "retention.ms=604800000,retention.bytes=1073741824,segment.bytes=536870912,compression.type=snappy,cleanup.policy=delete,min.insync.replicas=1,max.message.bytes=1048576"

# Tópico de baixa prioridade
create_topic "logs-low-priority" 2 1 \
    "retention.ms=259200000,retention.bytes=536870912,segment.bytes=268435456,compression.type=snappy,cleanup.policy=delete,min.insync.replicas=1,max.message.bytes=1048576"

# Tópico genérico (default)
create_topic "logs" 4 1 \
    "retention.ms=604800000,retention.bytes=1073741824,segment.bytes=536870912,compression.type=snappy,cleanup.policy=delete,min.insync.replicas=1,max.message.bytes=1048576"

# Tópico DLQ
create_topic "logs-dlq" 2 1 \
    "retention.ms=2592000000,retention.bytes=2147483648,segment.bytes=536870912,compression.type=gzip,cleanup.policy=delete,min.insync.replicas=1,max.message.bytes=2097152"

echo ""
echo "========================================="
echo "Topics Creation Complete!"
echo "========================================="
echo ""

# Lista todos os tópicos
echo "Current topics:"
kafka-topics --bootstrap-server $KAFKA_BROKER --list

echo ""
echo "========================================="
echo "Topic Details:"
echo "========================================="

# Mostra detalhes de cada tópico
for topic in "logs-high-priority" "logs-normal-priority" "logs-low-priority" "logs" "logs-dlq"; do
    echo ""
    echo "Topic: $topic"
    kafka-topics --bootstrap-server $KAFKA_BROKER --describe --topic $topic
done

echo ""
echo "========================================="
echo "Done! ✓"
echo "========================================="
