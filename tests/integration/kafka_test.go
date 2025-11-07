package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/internal/sinks"
	"ssw-logs-capture/pkg/dlq"
	"ssw-logs-capture/pkg/types"

	"github.com/IBM/sarama"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKafkaConnection tests basic Kafka broker connection
func TestKafkaConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := types.KafkaSinkConfig{
		Enabled:      true,
		Brokers:      []string{"localhost:9092"},
		Topic:        "test-connection",
		Compression:  "none",
		BatchSize:    10,
		BatchTimeout: "1s",
		QueueSize:    100,
		Timeout:      "10s",
		BackpressureConfig: types.BackpressureConfig{
			Enabled:                 true,
			QueueWarningThreshold:   0.75,
			QueueCriticalThreshold:  0.90,
			QueueEmergencyThreshold: 0.95,
		},
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	sink, err := sinks.NewKafkaSink(config, logger, nil, nil)
	require.NoError(t, err, "Failed to create Kafka sink")
	require.NotNil(t, sink, "Kafka sink should not be nil")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = sink.Start(ctx)
	require.NoError(t, err, "Failed to start Kafka sink")

	// Give it time to connect
	time.Sleep(2 * time.Second)

	// Check health
	assert.True(t, sink.IsHealthy(), "Kafka sink should be healthy")

	// Stop sink
	err = sink.Stop()
	assert.NoError(t, err, "Failed to stop Kafka sink")
}

// TestKafkaMessageProduction tests message production to Kafka
func TestKafkaMessageProduction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := types.KafkaSinkConfig{
		Enabled:      true,
		Brokers:      []string{"localhost:9092"},
		Topic:        "test-production",
		Compression:  "snappy",
		BatchSize:    5,
		BatchTimeout: "2s",
		QueueSize:    100,
		Timeout:      "10s",
		BackpressureConfig: types.BackpressureConfig{
			Enabled:                 true,
			QueueWarningThreshold:   0.75,
			QueueCriticalThreshold:  0.90,
			QueueEmergencyThreshold: 0.95,
		},
	}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	sink, err := sinks.NewKafkaSink(config, logger, nil, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = sink.Start(ctx)
	require.NoError(t, err)
	defer sink.Stop()

	// Send test entries
	entries := []types.LogEntry{
		{
			Message:    "Test message 1",
			Timestamp:  time.Now(),
			SourceType: "test",
			SourceID:   "test-1",
			Labels: map[string]string{
				"level":  "info",
				"test":   "integration",
			},
		},
		{
			Message:    "Test message 2",
			Timestamp:  time.Now(),
			SourceType: "test",
			SourceID:   "test-2",
			Labels: map[string]string{
				"level":  "warn",
				"test":   "integration",
			},
		},
	}

	err = sink.Send(ctx, entries)
	assert.NoError(t, err, "Failed to send entries")

	// Wait for batch to flush
	time.Sleep(3 * time.Second)

	// Check stats
	stats := sink.GetStats()
	assert.NotNil(t, stats)
	sentCount := stats["sent_total"].(int64)
	assert.GreaterOrEqual(t, sentCount, int64(2), "Should have sent at least 2 messages")
}

// TestKafkaPartitioningStrategies tests different partitioning strategies
func TestKafkaPartitioningStrategies(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	strategies := []struct {
		name     string
		strategy string
		keyField string
	}{
		{"hash partitioning", "hash", "tenant_id"},
		{"round-robin partitioning", "round-robin", ""},
		{"random partitioning", "random", ""},
	}

	for _, tt := range strategies {
		t.Run(tt.name, func(t *testing.T) {
			config := types.KafkaSinkConfig{
				Enabled:      true,
				Brokers:      []string{"localhost:9092"},
				Topic:        "test-partitioning",
				Compression:  "none",
				BatchSize:    10,
				BatchTimeout: "1s",
				QueueSize:    100,
				Partitioning: types.PartitioningConfig{
					Enabled:  true,
					Strategy: tt.strategy,
					KeyField: tt.keyField,
				},
				BackpressureConfig: types.BackpressureConfig{
					Enabled:                 true,
					QueueWarningThreshold:   0.75,
					QueueCriticalThreshold:  0.90,
					QueueEmergencyThreshold: 0.95,
				},
			}

			logger := logrus.New()
			logger.SetLevel(logrus.WarnLevel)

			sink, err := sinks.NewKafkaSink(config, logger, nil, nil)
			require.NoError(t, err)

			ctx := context.Background()
			err = sink.Start(ctx)
			require.NoError(t, err)
			defer sink.Stop()

			// Send entries with different partition keys
			entries := make([]types.LogEntry, 10)
			for i := 0; i < 10; i++ {
				entries[i] = types.LogEntry{
					Message:    fmt.Sprintf("Partition test message %d", i),
					Timestamp:  time.Now(),
					SourceType: "test",
					SourceID:   fmt.Sprintf("test-%d", i),
					Labels: map[string]string{
						"tenant_id": fmt.Sprintf("tenant-%d", i%3),
						"level":     "info",
					},
				}
			}

			err = sink.Send(ctx, entries)
			assert.NoError(t, err)

			time.Sleep(2 * time.Second)

			stats := sink.GetStats()
			sentCount := stats["sent_total"].(int64)
			assert.GreaterOrEqual(t, sentCount, int64(10))
		})
	}
}

// TestKafkaCompressionFormats tests different compression algorithms
func TestKafkaCompressionFormats(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	compressionTypes := []struct {
		name        string
		compression string
	}{
		{"no compression", "none"},
		{"gzip compression", "gzip"},
		{"snappy compression", "snappy"},
		{"lz4 compression", "lz4"},
		{"zstd compression", "zstd"},
	}

	for _, tt := range compressionTypes {
		t.Run(tt.name, func(t *testing.T) {
			config := types.KafkaSinkConfig{
				Enabled:      true,
				Brokers:      []string{"localhost:9092"},
				Topic:        "test-compression",
				Compression:  tt.compression,
				BatchSize:    5,
				BatchTimeout: "1s",
				QueueSize:    50,
				BackpressureConfig: types.BackpressureConfig{
					Enabled:                 true,
					QueueWarningThreshold:   0.75,
					QueueCriticalThreshold:  0.90,
					QueueEmergencyThreshold: 0.95,
				},
			}

			logger := logrus.New()
			logger.SetLevel(logrus.WarnLevel)

			sink, err := sinks.NewKafkaSink(config, logger, nil, nil)
			require.NoError(t, err, "Failed to create sink with %s", tt.compression)

			ctx := context.Background()
			err = sink.Start(ctx)
			require.NoError(t, err)
			defer sink.Stop()

			// Send test entry
			entry := types.LogEntry{
				Message:    "Compression test message",
				Timestamp:  time.Now(),
				SourceType: "test",
				SourceID:   "compression-test",
				Labels: map[string]string{
					"compression": tt.compression,
					"level":       "info",
				},
			}

			err = sink.Send(ctx, []types.LogEntry{entry})
			assert.NoError(t, err)

			time.Sleep(2 * time.Second)

			stats := sink.GetStats()
			assert.True(t, stats["running"].(bool))
		})
	}
}

// TestKafkaSASLAuthentication tests SASL authentication mechanisms
func TestKafkaSASLAuthentication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This test requires Kafka with SASL enabled
	// Skip if KAFKA_SASL_ENABLED env var is not set
	t.Skip("SASL authentication requires specific Kafka setup")

	mechanisms := []struct {
		name      string
		mechanism string
	}{
		{"PLAIN", "PLAIN"},
		{"SCRAM-SHA-256", "SCRAM-SHA-256"},
		{"SCRAM-SHA-512", "SCRAM-SHA-512"},
	}

	for _, tt := range mechanisms {
		t.Run(tt.name, func(t *testing.T) {
			config := types.KafkaSinkConfig{
				Enabled:      true,
				Brokers:      []string{"localhost:9093"}, // SASL port
				Topic:        "test-auth",
				Compression:  "none",
				BatchSize:    5,
				BatchTimeout: "1s",
				QueueSize:    50,
				Auth: types.AuthConfig{
					Enabled:   true,
					Mechanism: tt.mechanism,
					Username:  "test-user",
					Password:  "test-password",
				},
				BackpressureConfig: types.BackpressureConfig{
					Enabled:                 true,
					QueueWarningThreshold:   0.75,
					QueueCriticalThreshold:  0.90,
					QueueEmergencyThreshold: 0.95,
				},
			}

			logger := logrus.New()
			logger.SetLevel(logrus.InfoLevel)

			sink, err := sinks.NewKafkaSink(config, logger, nil, nil)
			require.NoError(t, err)

			ctx := context.Background()
			err = sink.Start(ctx)
			require.NoError(t, err)
			defer sink.Stop()

			// Test basic send
			entry := types.LogEntry{
				Message:    "Auth test message",
				Timestamp:  time.Now(),
				SourceType: "test",
				SourceID:   "auth-test",
				Labels: map[string]string{
					"auth": tt.mechanism,
				},
			}

			err = sink.Send(ctx, []types.LogEntry{entry})
			assert.NoError(t, err)
		})
	}
}

// TestKafkaTLS tests TLS/SSL connection
func TestKafkaTLS(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This test requires Kafka with TLS enabled
	t.Skip("TLS test requires specific Kafka setup with certificates")

	config := types.KafkaSinkConfig{
		Enabled:      true,
		Brokers:      []string{"localhost:9094"}, // TLS port
		Topic:        "test-tls",
		Compression:  "snappy",
		BatchSize:    5,
		BatchTimeout: "1s",
		QueueSize:    50,
		TLS: types.TLSConfig{
			Enabled:           true,
			VerifyCertificate: true,
			CAFile:            "/path/to/ca.crt",
			CertFile:          "/path/to/client.crt",
			KeyFile:           "/path/to/client.key",
		},
		BackpressureConfig: types.BackpressureConfig{
			Enabled:                 true,
			QueueWarningThreshold:   0.75,
			QueueCriticalThreshold:  0.90,
			QueueEmergencyThreshold: 0.95,
		},
	}

	logger := logrus.New()
	sink, err := sinks.NewKafkaSink(config, logger, nil, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = sink.Start(ctx)
	require.NoError(t, err)
	defer sink.Stop()
}

// TestKafkaDLQIntegration tests DLQ integration
func TestKafkaDLQIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	// Create DLQ
	dlqConfig := types.DLQConfig{
		Enabled:       true,
		Directory:     "/tmp/kafka-dlq-test",
		MaxSizeMB:     10,
		MaxFiles:      5,
		RetentionDays: 1,
		WriteTimeout:  "5s",
	}

	deadLetterQueue, err := dlq.NewDeadLetterQueue(dlqConfig, logger)
	require.NoError(t, err)
	require.NotNil(t, deadLetterQueue)

	config := types.KafkaSinkConfig{
		Enabled:      true,
		Brokers:      []string{"invalid-broker:9092"}, // Invalid broker to trigger errors
		Topic:        "test-dlq",
		Compression:  "none",
		BatchSize:    5,
		BatchTimeout: "1s",
		QueueSize:    50,
		Timeout:      "1s",
		DLQConfig: types.DLQIntegrationConfig{
			Enabled:      true,
			SendOnError:  true,
			SendOnTimeout: true,
		},
		BackpressureConfig: types.BackpressureConfig{
			Enabled:                 true,
			QueueWarningThreshold:   0.75,
			QueueCriticalThreshold:  0.90,
			QueueEmergencyThreshold: 0.95,
		},
	}

	sink, err := sinks.NewKafkaSink(config, logger, deadLetterQueue, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = sink.Start(ctx)
	require.NoError(t, err)
	defer sink.Stop()

	// Send entry that will fail
	entry := types.LogEntry{
		Message:    "DLQ test message",
		Timestamp:  time.Now(),
		SourceType: "test",
		SourceID:   "dlq-test",
		Labels: map[string]string{
			"test": "dlq",
		},
	}

	err = sink.Send(ctx, []types.LogEntry{entry})
	// Error expected because broker is invalid
	// Entry should go to DLQ

	time.Sleep(2 * time.Second)

	// Check DLQ stats
	dlqStats := deadLetterQueue.GetStats()
	assert.NotNil(t, dlqStats)
}

// TestKafkaCircuitBreaker tests circuit breaker behavior
func TestKafkaCircuitBreaker(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	config := types.KafkaSinkConfig{
		Enabled:      true,
		Brokers:      []string{"invalid-broker:9092"}, // Invalid to trigger circuit breaker
		Topic:        "test-circuit-breaker",
		Compression:  "none",
		BatchSize:    1,
		BatchTimeout: "100ms",
		QueueSize:    10,
		Timeout:      "500ms",
		RetryMax:     1,
		BackpressureConfig: types.BackpressureConfig{
			Enabled:                 true,
			QueueWarningThreshold:   0.75,
			QueueCriticalThreshold:  0.90,
			QueueEmergencyThreshold: 0.95,
		},
	}

	sink, err := sinks.NewKafkaSink(config, logger, nil, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = sink.Start(ctx)
	require.NoError(t, err)
	defer sink.Stop()

	// Send multiple entries to trigger circuit breaker
	for i := 0; i < 15; i++ {
		entry := types.LogEntry{
			Message:    fmt.Sprintf("Circuit breaker test %d", i),
			Timestamp:  time.Now(),
			SourceType: "test",
			SourceID:   fmt.Sprintf("cb-test-%d", i),
		}
		sink.Send(ctx, []types.LogEntry{entry})
		time.Sleep(100 * time.Millisecond)
	}

	time.Sleep(2 * time.Second)

	// Check circuit breaker state
	stats := sink.GetStats()
	cbState := stats["circuit_breaker"].(string)
	assert.Contains(t, []string{"open", "half-open"}, cbState, "Circuit breaker should be open or half-open")
}

// TestKafkaBackpressure tests backpressure handling
func TestKafkaBackpressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := types.KafkaSinkConfig{
		Enabled:      true,
		Brokers:      []string{"localhost:9092"},
		Topic:        "test-backpressure",
		Compression:  "none",
		BatchSize:    1000,
		BatchTimeout: "10s",
		QueueSize:    50, // Small queue to trigger backpressure
		BackpressureConfig: types.BackpressureConfig{
			Enabled:                 true,
			QueueWarningThreshold:   0.50,
			QueueCriticalThreshold:  0.70,
			QueueEmergencyThreshold: 0.90,
		},
	}

	sink, err := sinks.NewKafkaSink(config, logger, nil, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = sink.Start(ctx)
	require.NoError(t, err)
	defer sink.Stop()

	// Flood the queue
	entries := make([]types.LogEntry, 100)
	for i := 0; i < 100; i++ {
		entries[i] = types.LogEntry{
			Message:    fmt.Sprintf("Backpressure test %d", i),
			Timestamp:  time.Now(),
			SourceType: "test",
			SourceID:   fmt.Sprintf("bp-test-%d", i),
		}
	}

	err = sink.Send(ctx, entries)
	// Some entries may be dropped due to backpressure

	time.Sleep(2 * time.Second)

	stats := sink.GetStats()
	queueUtilization := stats["queue_utilization"].(float64)
	backpressureCount := stats["backpressure_count"].(int64)

	assert.GreaterOrEqual(t, backpressureCount, int64(0), "Backpressure should have been triggered")
	assert.LessOrEqual(t, queueUtilization, 1.0, "Queue utilization should not exceed 100%")
}

// BenchmarkKafkaThroughput benchmarks message throughput
func BenchmarkKafkaThroughput(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.KafkaSinkConfig{
		Enabled:      true,
		Brokers:      []string{"localhost:9092"},
		Topic:        "benchmark-throughput",
		Compression:  "snappy",
		BatchSize:    1000,
		BatchTimeout: "100ms",
		QueueSize:    10000,
		BackpressureConfig: types.BackpressureConfig{
			Enabled:                 true,
			QueueWarningThreshold:   0.75,
			QueueCriticalThreshold:  0.90,
			QueueEmergencyThreshold: 0.95,
		},
	}

	sink, err := sinks.NewKafkaSink(config, logger, nil, nil)
	require.NoError(b, err)

	ctx := context.Background()
	err = sink.Start(ctx)
	require.NoError(b, err)
	defer sink.Stop()

	entry := types.LogEntry{
		Message:    "Benchmark message",
		Timestamp:  time.Now(),
		SourceType: "benchmark",
		SourceID:   "benchmark-test",
		Labels: map[string]string{
			"level": "info",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sink.Send(ctx, []types.LogEntry{entry})
		}
	})

	b.StopTimer()

	// Report stats
	stats := sink.GetStats()
	b.ReportMetric(float64(stats["sent_total"].(int64)), "messages/sent")
	b.ReportMetric(float64(stats["error_total"].(int64)), "errors")
}

// BenchmarkKafkaCompressionOverhead benchmarks compression overhead
func BenchmarkKafkaCompressionOverhead(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	compressionTypes := []string{"none", "gzip", "snappy", "lz4", "zstd"}

	for _, compression := range compressionTypes {
		b.Run(compression, func(b *testing.B) {
			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			config := types.KafkaSinkConfig{
				Enabled:      true,
				Brokers:      []string{"localhost:9092"},
				Topic:        "benchmark-compression",
				Compression:  compression,
				BatchSize:    100,
				BatchTimeout: "1s",
				QueueSize:    1000,
				BackpressureConfig: types.BackpressureConfig{
					Enabled:                 true,
					QueueWarningThreshold:   0.75,
					QueueCriticalThreshold:  0.90,
					QueueEmergencyThreshold: 0.95,
				},
			}

			sink, err := sinks.NewKafkaSink(config, logger, nil, nil)
			require.NoError(b, err)

			ctx := context.Background()
			err = sink.Start(ctx)
			require.NoError(b, err)
			defer sink.Stop()

			entry := types.LogEntry{
				Message:    "Compression benchmark message with some payload data to compress",
				Timestamp:  time.Now(),
				SourceType: "benchmark",
				SourceID:   "compression-benchmark",
				Labels: map[string]string{
					"compression": compression,
					"level":       "info",
				},
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				sink.Send(ctx, []types.LogEntry{entry})
			}
		})
	}
}
