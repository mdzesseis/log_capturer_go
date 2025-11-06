package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"ssw-logs-capture/internal/config"
	"ssw-logs-capture/internal/monitors"
	"ssw-logs-capture/pkg/positions"
	"ssw-logs-capture/pkg/selfguard"
	"ssw-logs-capture/pkg/task_manager"
	"ssw-logs-capture/pkg/types"
	"ssw-logs-capture/pkg/validation"

	"github.com/sirupsen/logrus"
)

// MockDispatcher para capturar logs enviados
type MockDispatcher struct {
	receivedCount int
	lastMessage   string
	lastSourceID  string
}

func (d *MockDispatcher) Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
	d.receivedCount++
	d.lastMessage = message
	d.lastSourceID = sourceID

	fmt.Printf("📨 [DISPATCHER] Received log #%d:\n", d.receivedCount)
	fmt.Printf("   Source Type: %s\n", sourceType)
	fmt.Printf("   Source ID: %s\n", sourceID)
	fmt.Printf("   Message: %s\n", message)
	if len(labels) > 0 {
		fmt.Printf("   Labels:\n")
		for k, v := range labels {
			fmt.Printf("      %s: %s\n", k, v)
		}
	}
	fmt.Println()
	return nil
}

func main() {
	fmt.Println("========================================")
	fmt.Println("🔍 FILE MONITOR VALIDATION TEST")
	fmt.Println("========================================\n")

	// Configurar logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "15:04:05",
		ForceColors:     true,
	})

	// Criar arquivo de teste
	testFile := "/tmp/test_monitor.log"
	fmt.Printf("📝 Creating test file: %s\n", testFile)
	err := ioutil.WriteFile(testFile, []byte("Initial test log entry\n"), 0644)
	if err != nil {
		fmt.Printf("❌ Failed to create test file: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(testFile)

	// Carregar configuração
	fmt.Println("\n📋 Loading configuration...")
	cfg, err := config.LoadConfig(
		"/home/mateus/log_capturer_go/configs/config.yaml",
		"/home/mateus/log_capturer_go/configs/pipelines.yaml",
		"/home/mateus/log_capturer_go/configs/file_pipeline.yml",
	)
	if err != nil {
		fmt.Printf("❌ Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Mostrar configuração
	fmt.Printf("✅ FileMonitor enabled: %v\n", cfg.FileMonitorService.Enabled)
	fmt.Printf("✅ Pipeline file: %s\n", cfg.FileMonitorService.PipelineFile)

	if cfg.FileMonitorService.PipelineConfig != nil {
		// Contar arquivos configurados
		if filesInterface, ok := cfg.FileMonitorService.PipelineConfig["files"]; ok {
			if filesSlice, ok := filesInterface.([]interface{}); ok {
				fmt.Printf("✅ Files configured in pipeline: %d\n", len(filesSlice))

				// Listar arquivos
				fmt.Println("\n📁 Files to be monitored:")
				for i, fileInterface := range filesSlice {
					if fileMap, ok := fileInterface.(map[string]interface{}); ok {
						if pathInterface, ok := fileMap["path"]; ok {
							if path, ok := pathInterface.(string); ok {
								// Verificar se arquivo existe
								if _, err := os.Stat(path); err == nil {
									fmt.Printf("   %d. %s ✅ (exists)\n", i+1, path)
								} else {
									fmt.Printf("   %d. %s ❌ (not found)\n", i+1, path)
								}
							}
						}
					}
				}
			}
		}
	}

	// Adicionar nosso arquivo de teste à configuração
	fmt.Printf("\n➕ Adding test file to monitor: %s\n", testFile)

	// Criar componentes necessários
	dispatcher := &MockDispatcher{}
	taskMgr := task_manager.New(types.TaskManagerConfig{
		HeartbeatInterval: 30 * time.Second,
		TaskTimeout:       5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
	}, logger)

	positionMgr := positions.NewPositionBufferManager(
		types.PositionManagerConfig{
			FilePath:      "/tmp/test_positions.json",
			FlushInterval: 5 * time.Second,
			BufferSize:    100,
			Enabled:       true,
		},
		logger,
	)

	timestampValidator := validation.NewTimestampValidator(
		cfg.TimestampValidation,
		logger,
	)

	feedbackGuard := selfguard.NewFeedbackGuard(
		types.FeedbackGuardConfig{
			Enabled:    true,
			Threshold:  10,
			WindowSize: 60 * time.Second,
		},
		logger,
	)

	// Criar FileMonitor
	fmt.Println("\n🚀 Creating FileMonitor...")
	fm, err := monitors.NewFileMonitor(
		cfg.FileMonitorService,
		cfg.TimestampValidation,
		dispatcher,
		taskMgr,
		positionMgr,
		logger,
	)
	if err != nil {
		fmt.Printf("❌ Failed to create file monitor: %v\n", err)
		os.Exit(1)
	}

	// Adicionar arquivo de teste manualmente
	labels := map[string]string{
		"source":    "test",
		"test_file": "true",
	}
	err = fm.AddFile(testFile, labels)
	if err != nil {
		fmt.Printf("⚠️ Warning adding test file: %v\n", err)
	} else {
		fmt.Printf("✅ Test file added to monitoring\n")
	}

	// Criar contexto com timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Iniciar FileMonitor
	fmt.Println("\n🎯 Starting FileMonitor...")
	err = fm.Start(ctx)
	if err != nil {
		fmt.Printf("❌ Failed to start file monitor: %v\n", err)
		os.Exit(1)
	}

	// Aguardar descoberta de arquivos
	fmt.Println("⏳ Waiting for file discovery (5 seconds)...")
	time.Sleep(5 * time.Second)

	// Escrever no arquivo de teste
	fmt.Printf("\n✍️ Writing to test file: %s\n", testFile)
	f, err := os.OpenFile(testFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("❌ Failed to open test file: %v\n", err)
	} else {
		testMessages := []string{
			"Test log entry 1 - Testing file monitor",
			"Test log entry 2 - Validation in progress",
			"Test log entry 3 - File monitor is working!",
		}

		for i, msg := range testMessages {
			fmt.Printf("   Writing message %d: %s\n", i+1, msg)
			f.WriteString(msg + "\n")
			time.Sleep(1 * time.Second)
		}
		f.Close()
	}

	// Aguardar processamento
	fmt.Println("\n⏳ Waiting for log processing (5 seconds)...")
	time.Sleep(5 * time.Second)

	// Parar FileMonitor
	fmt.Println("\n🛑 Stopping FileMonitor...")
	err = fm.Stop()
	if err != nil {
		fmt.Printf("⚠️ Warning stopping file monitor: %v\n", err)
	}

	// Parar task manager
	taskMgr.Shutdown()

	// Mostrar resultados
	fmt.Println("\n========================================")
	fmt.Println("📊 VALIDATION RESULTS")
	fmt.Println("========================================")

	if dispatcher.receivedCount > 0 {
		fmt.Printf("✅ SUCCESS: FileMonitor is working!\n")
		fmt.Printf("   Total logs captured: %d\n", dispatcher.receivedCount)
		fmt.Printf("   Last message: %s\n", dispatcher.lastMessage)
		fmt.Printf("   Last source: %s\n", dispatcher.lastSourceID)
	} else {
		fmt.Printf("❌ FAILURE: No logs were captured\n")
		fmt.Println("   Possible issues:")
		fmt.Println("   - FileMonitor not discovering files")
		fmt.Println("   - Files not being read properly")
		fmt.Println("   - Dispatcher not receiving events")
	}

	fmt.Println("\n✅ Test completed!")
}