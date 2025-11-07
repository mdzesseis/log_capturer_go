#!/bin/bash
# FASE 6: Collect Baseline Metrics

cd /home/mateus/log_capturer_go

echo "=== BASELINE METRICS (BEFORE LOAD TEST) ===" | tee fase6_baseline.txt
date "+%Y-%m-%d %H:%M:%S" | tee -a fase6_baseline.txt

# Goroutines
GOROUTINES=$(curl -s http://localhost:8001/metrics | grep "^log_capturer_goroutines " | awk '{print $2}')
echo "Goroutines: $GOROUTINES" | tee -a fase6_baseline.txt

# Memory
MEMORY=$(curl -s http://localhost:8001/metrics | grep "^log_capturer_memory_usage_bytes " | awk '{print $2}')
MEMORY_MB=$(echo "scale=2; $MEMORY / 1048576" | bc)
echo "Memory: $MEMORY_MB MB" | tee -a fase6_baseline.txt

# Active streams
STREAMS=$(curl -s http://localhost:8001/metrics | grep "^log_capturer_container_streams_active " | awk '{print $2}')
echo "Active Streams: $STREAMS" | tee -a fase6_baseline.txt

# File descriptors
FDS=$(curl -s http://localhost:8001/metrics | grep "^log_capturer_file_descriptors_open " | awk '{print $2}')
echo "File Descriptors: $FDS" | tee -a fase6_baseline.txt

# Logs processed
LOGS=$(curl -s http://localhost:8001/metrics | grep "^log_capturer_logs_processed_total" | head -1 | awk '{print $2}')
echo "Logs Processed: $LOGS" | tee -a fase6_baseline.txt

# CPU
CPU=$(curl -s http://localhost:8001/metrics | grep "^log_capturer_cpu_usage_percent " | awk '{print $2}')
echo "CPU: $CPU%" | tee -a fase6_baseline.txt

# Container count (current)
CONTAINERS=$(docker ps --format '{{.Names}}' | wc -l)
echo "Docker Containers: $CONTAINERS" | tee -a fase6_baseline.txt

cat fase6_baseline.txt
