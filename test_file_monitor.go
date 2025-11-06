package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"
    "sync"
    "ssw-logs-capture/internal/config"
    "ssw-logs-capture/internal/monitors"
    "ssw-logs-capture/pkg/task_manager"
    "ssw-logs-capture/pkg/positions"
    "github.com/sirupsen/logrus"
)

const testFile = "/tmp/test_monitor.log"

func main() {
    fmt.Println("=== FILE MONITOR TEST ===")
    fmt.Println("Testing file monitor functionality...")

    logger := logrus.New()
    logger.SetLevel(logrus.DebugLevel)
    logger.SetFormatter(&logrus.TextFormatter{
        FullTimestamp: true,
        TimestampFormat: "15:04:05.000",
    })

    // Create test file first
    fmt.Printf("\n1. Creating test file: %s\n", testFile)
    file, err := os.Create(testFile)
    if err != nil {
        log.Fatalf("Failed to create test file: %v", err)
    }

    // Write initial content
    initialLines := []string{
        "Initial test line 1",
        "Initial test line 2",
        "Initial test line 3",
    }
    for _, line := range initialLines {
        file.WriteString(line + "\n")
    }
    file.Sync()
    file.Close()
    fmt.Println("   ✓ Test file created with initial content")

    // Load config
    fmt.Println("\n2. Loading configuration...")
    cfg, err := config.LoadConfig("/home/mateus/log_capturer_go/configs/config.yaml", "/home/mateus/log_capturer_go/configs/pipelines.yaml", "/home/mateus/log_capturer_go/configs/file_pipeline.yml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }

    fmt.Printf("   FileMonitor config: Enabled=%v, PipelineFile=%s\n",
        cfg.FileMonitorService.Enabled,
        cfg.FileMonitorService.PipelineFile)

    if cfg.FileMonitorService.PipelineConfig != nil {
        fmt.Println("   ✓ Pipeline config loaded successfully")
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Create mock dispatcher
    dispatcher := &mockDispatcher{
        logs: make([]string, 0),
    }

    // Create task manager
    taskManager := task_manager.NewTaskManager(logger)

    // Create position manager
    positionManager := positions.NewPositionBufferManager("/tmp/positions.db", logger)

    // Create file monitor
    fmt.Println("\n3. Creating file monitor...")
    fm, err := monitors.NewFileMonitor(
        cfg.FileMonitorService,
        cfg.TimestampValidation,
        dispatcher,
        taskManager,
        positionManager,
        logger,
    )
    if err != nil {
        log.Fatalf("Failed to create file monitor: %v", err)
    }
    fmt.Println("   ✓ File monitor created")

    // Start monitor
    fmt.Println("\n4. Starting file monitor...")
    err = fm.Start(ctx)
    if err != nil {
        log.Fatalf("Failed to start file monitor: %v", err)
    }
    fmt.Println("   ✓ File monitor started")

    // Add test file explicitly
    fmt.Printf("\n5. Adding test file to monitor: %s\n", testFile)
    labels := map[string]string{
        "test": "true",
        "source": "test_file",
    }
    if err := fm.AddFile(testFile, labels); err != nil {
        log.Printf("Warning: Failed to add test file: %v", err)
    } else {
        fmt.Println("   ✓ Test file added to monitoring")
    }

    // Wait for initial processing
    fmt.Println("\n6. Waiting for initial file processing...")
    time.Sleep(3 * time.Second)

    // Check if initial content was captured
    fmt.Printf("   Logs captured so far: %d\n", len(dispatcher.logs))
    for i, logMsg := range dispatcher.logs {
        fmt.Printf("     %d. %s\n", i+1, logMsg)
    }

    // Write new content to file
    fmt.Println("\n7. Writing new content to test file...")
    file, err = os.OpenFile(testFile, os.O_APPEND|os.O_WRONLY, 0644)
    if err != nil {
        log.Fatalf("Failed to open file for append: %v", err)
    }

    newLines := []string{
        "New log line 1 - " + time.Now().Format("15:04:05"),
        "New log line 2 - " + time.Now().Format("15:04:05"),
        "ERROR: This is an error message",
        "INFO: This is an info message",
        "DEBUG: This is a debug message",
    }

    for _, line := range newLines {
        file.WriteString(line + "\n")
        fmt.Printf("   → Written: %s\n", line)
    }
    file.Sync()
    file.Close()

    // Wait for file monitor to detect changes
    fmt.Println("\n8. Waiting for file monitor to detect new content...")
    time.Sleep(5 * time.Second)

    // Final results
    fmt.Printf("\n9. FINAL RESULTS:\n")
    fmt.Printf("   Total logs captured: %d\n", len(dispatcher.logs))
    if len(dispatcher.logs) > 0 {
        fmt.Println("   All captured logs:")
        for i, logMsg := range dispatcher.logs {
            fmt.Printf("     %d. %s\n", i+1, logMsg)
        }
        fmt.Println("\n   ✓ SUCCESS: File monitor is capturing logs")
    } else {
        fmt.Println("\n   ✗ FAILURE: No logs were captured")
        fmt.Println("\n   Possible issues:")
        fmt.Println("   - File monitor not detecting file changes")
        fmt.Println("   - Dispatcher not receiving logs")
        fmt.Println("   - File reading issues")
    }

    // Check monitored files
    fmt.Println("\n10. Currently monitored files:")
    monitoredFiles := fm.GetMonitoredFiles()
    if len(monitoredFiles) > 0 {
        for _, mf := range monitoredFiles {
            fmt.Printf("   - %s (task: %s)\n", mf["filepath"], mf["task_name"])
        }
    } else {
        fmt.Println("   No files being monitored!")
    }

    // Stop
    fmt.Println("\n11. Stopping file monitor...")
    fm.Stop()
    taskManager.StopAll()
    fmt.Println("   ✓ File monitor stopped")

    // Cleanup
    os.Remove(testFile)
    fmt.Println("\n=== TEST COMPLETE ===")
}

type mockDispatcher struct {
    logs []string
    mu sync.Mutex
}

func (d *mockDispatcher) Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.logs = append(d.logs, message)
    log.Printf("[DISPATCHER] Captured: source=%s, id=%s, message=%s", sourceType, sourceID, message)
    return nil
}
