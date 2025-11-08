package monitors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ssw-logs-capture/pkg/types"
)

// ===================================================================================
// Test Helpers (shared helpers in test_helpers.go)
// ===================================================================================

// createTestFile creates a temporary file for testing
func createTestFile(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.log")

	file, err := os.Create(filePath)
	require.NoError(t, err)
	defer file.Close()

	return filePath
}

// ===================================================================================
// TAREFA 2: Testes de Construtor
// ===================================================================================

func TestNewFileMonitor_Success(t *testing.T) {
	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{"/tmp/test.log"},
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(
		config,
		dispatcher,
		taskManager,
		nil, // positionManager
		logger,
	)

	require.NoError(t, err)
	require.NotNil(t, fm)
	assert.NotNil(t, fm.logger)
	assert.NotNil(t, fm.dispatcher)
	assert.Equal(t, config.WatchDirectories, fm.config.WatchDirectories)
	assert.NotNil(t, fm.tailers)
	assert.False(t, fm.running)
}

func TestNewFileMonitor_NilLogger(t *testing.T) {
	config := types.FileMonitorServiceConfig{}
	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}

	_, err := NewFileMonitor(
		config,
		dispatcher,
		taskManager,
		nil,
		nil, // logger nil
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "logger é obrigatório")
}

func TestNewFileMonitor_NilDispatcher(t *testing.T) {
	config := types.FileMonitorServiceConfig{}
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	_, err := NewFileMonitor(
		config,
		nil, // dispatcher nil
		taskManager,
		nil,
		logger,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dispatcher é obrigatório")
}

func TestNewFileMonitor_EmptyConfig(t *testing.T) {
	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{},
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(
		config,
		dispatcher,
		taskManager,
		nil,
		logger,
	)

	require.NoError(t, err, "Constructor should succeed even with empty config")
	require.NotNil(t, fm)
}

// ===================================================================================
// TAREFA 3: Testes de Start/Stop
// ===================================================================================

func TestFileMonitor_StartStop(t *testing.T) {
	testFile := createTestFile(t)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)

	// Start
	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	// Aguardar inicialização
	time.Sleep(500 * time.Millisecond)

	// Verificar estado
	fm.runningMux.RLock()
	running := fm.running
	fm.runningMux.RUnlock()
	assert.True(t, running, "FileMonitor should be running after Start")

	// Stop
	err = fm.Stop()
	require.NoError(t, err)

	// Verificar estado
	fm.runningMux.RLock()
	running = fm.running
	fm.runningMux.RUnlock()
	assert.False(t, running, "FileMonitor should be stopped after Stop")
}

func TestFileMonitor_PreventDoubleStart(t *testing.T) {
	testFile := createTestFile(t)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()

	// Primeiro Start
	err = fm.Start(ctx)
	require.NoError(t, err)

	// Aguardar inicialização
	time.Sleep(200 * time.Millisecond)

	// Segundo Start deve falhar
	err = fm.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "já está em execução")
}

func TestFileMonitor_StopWithoutStart(t *testing.T) {
	testFile := createTestFile(t)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)

	// Stop without Start should not error
	err = fm.Stop()
	assert.NoError(t, err)
}

func TestFileMonitor_MultipleStopCalls(t *testing.T) {
	testFile := createTestFile(t)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// First Stop
	err = fm.Stop()
	assert.NoError(t, err)

	// Second Stop should not error
	err = fm.Stop()
	assert.NoError(t, err)
}

func TestFileMonitor_StartWithNoFiles(t *testing.T) {
	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{},
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)
	require.Error(t, err, "Should fail when no files configured")
}

// ===================================================================================
// TAREFA 4: Testes de Leitura de Arquivo
// ===================================================================================

func TestFileMonitor_ReadExistingFile(t *testing.T) {
	testFile := createTestFile(t)

	// Escrever linhas ANTES de iniciar monitor
	writeToFile(t, testFile, "linha 1", "linha 2", "linha 3")

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
		SeekStrategy:     "beginning", // Lê desde o início
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	// Aguardar processamento com timeout
	success := waitForCondition(t, 5*time.Second, func() bool {
		return dispatcher.GetCallCount() >= 3
	})

	assert.True(t, success, "Should process at least 3 lines within timeout")

	// Verificar que linhas foram processadas
	calls := dispatcher.GetCalls()
	assert.GreaterOrEqual(t, len(calls), 3, "Should have processed at least 3 lines")

	for _, call := range calls {
		assert.Equal(t, "file", call.SourceType)
		assert.Equal(t, testFile, call.SourceID)
		assert.NotEmpty(t, call.Message)
		assert.NotNil(t, call.Labels)
	}
}

func TestFileMonitor_TailNewLines(t *testing.T) {
	testFile := createTestFile(t)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
		SeekStrategy:     "end", // Começa do fim (tail -f)
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	// Aguardar monitor inicializar
	time.Sleep(1 * time.Second)

	initialCount := dispatcher.GetCallCount()

	// Escrever novas linhas DEPOIS de iniciar
	writeToFile(t, testFile, "nova linha 1", "nova linha 2")

	// Aguardar processamento
	success := waitForCondition(t, 5*time.Second, func() bool {
		return dispatcher.GetCallCount() >= initialCount+2
	})

	assert.True(t, success, "Should process new lines within timeout")

	// Verificar que novas linhas foram processadas
	finalCount := dispatcher.GetCallCount()
	newLines := finalCount - initialCount

	assert.GreaterOrEqual(t, newLines, 2, "Should process at least 2 new lines")
}

func TestFileMonitor_SeekStrategy_Recent(t *testing.T) {
	testFile := createTestFile(t)

	// Escrever muitas linhas
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeToFile(t, testFile, lines...)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
		SeekStrategy:     "recent",
		SeekRecentBytes:  500, // Apenas últimos 500 bytes
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	// Aguardar processamento
	time.Sleep(2 * time.Second)

	// Deve processar menos que todas as linhas (apenas as recentes)
	callCount := dispatcher.GetCallCount()
	assert.Less(t, callCount, 100, "Should process less than all 100 lines with 'recent' strategy")
	assert.Greater(t, callCount, 0, "Should process some lines")
}

func TestFileMonitor_IgnoreOldTimestamps(t *testing.T) {
	testFile := createTestFile(t)

	// Escrever linhas antigas
	writeToFile(t, testFile, "old line 1", "old line 2")

	config := types.FileMonitorServiceConfig{
		WatchDirectories:    []string{testFile},
		IgnoreOldTimestamps: true, // Ignora linhas existentes
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	// Aguardar inicialização
	time.Sleep(1 * time.Second)

	initialCount := dispatcher.GetCallCount()

	// Escrever nova linha
	writeToFile(t, testFile, "new line")

	// Aguardar processamento
	success := waitForCondition(t, 5*time.Second, func() bool {
		return dispatcher.GetCallCount() > initialCount
	})

	assert.True(t, success, "Should process new line")

	// Deve ignorar linhas antigas
	assert.Equal(t, 0, initialCount, "Should ignore old lines with IgnoreOldTimestamps")
}

// ===================================================================================
// TAREFA 5: Testes de Race Conditions
// ===================================================================================

func TestFileMonitor_ConcurrentFileWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	testFile := createTestFile(t)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
		SeekStrategy:     "beginning",
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Múltiplas goroutines escrevendo concorrentemente
	const numWriters = 5
	const linesPerWriter = 10
	var wg sync.WaitGroup
	wg.Add(numWriters)

	for i := 0; i < numWriters; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < linesPerWriter; j++ {
				writeToFile(t, testFile, fmt.Sprintf("writer-%d line-%d", id, j))
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(3 * time.Second)

	// Verificar que não houve race conditions (teste passa com -race flag)
	callCount := dispatcher.GetCallCount()
	t.Logf("Processed %d lines from %d expected", callCount, numWriters*linesPerWriter)

	assert.Greater(t, callCount, 0, "Should process some lines")
}

func TestFileMonitor_ConcurrentStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	testFile := createTestFile(t)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Múltiplas goroutines tentando Start/Stop
	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			fm.Start(ctx)
			time.Sleep(10 * time.Millisecond)
			fm.Stop()
		}()
	}

	// Aguardar todas
	wg.Wait()

	// Cleanup final
	fm.Stop()
}

func TestFileMonitor_ConcurrentDispatcherAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	testFile := createTestFile(t)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
		SeekStrategy:     "beginning",
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// Escrever linhas para processamento
	for i := 0; i < 50; i++ {
		writeToFile(t, testFile, fmt.Sprintf("test line %d", i))
	}

	// Concorrentemente acessar dispatcher
	var wg sync.WaitGroup
	wg.Add(10)

	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				dispatcher.GetCallCount()
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Não deve causar race conditions
	callCount := dispatcher.GetCallCount()
	t.Logf("Final call count: %d", callCount)
}

// ===================================================================================
// TAREFA 6: Testes de Worker Pool
// ===================================================================================

func TestWorkerPool_ProcessLogLine(t *testing.T) {
	ctx := context.Background()
	dispatcher := NewMockDispatcher()
	logger := newTestLogger()

	pool := newWorkerPool(ctx, 2, 10, dispatcher, logger)
	defer pool.close()

	// Enviar job ao pool
	job := &workerJob{
		line:       "test log line",
		sourcePath: "/tmp/test.log",
		timestamp:  time.Now(),
	}

	pool.jobsChannel <- job

	// Aguardar processamento
	time.Sleep(500 * time.Millisecond)

	// Verificar que foi processado
	calls := dispatcher.GetCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "file", calls[0].SourceType)
	assert.Equal(t, "/tmp/test.log", calls[0].SourceID)
	assert.Equal(t, "test log line", calls[0].Message)
	assert.NotNil(t, calls[0].Labels)
}

func TestWorkerPool_MultipleJobs(t *testing.T) {
	ctx := context.Background()
	dispatcher := NewMockDispatcher()
	logger := newTestLogger()

	pool := newWorkerPool(ctx, 4, 100, dispatcher, logger)
	defer pool.close()

	// Enviar múltiplos jobs
	const numJobs = 20
	for i := 0; i < numJobs; i++ {
		job := &workerJob{
			line:       fmt.Sprintf("log line %d", i),
			sourcePath: "/tmp/test.log",
			timestamp:  time.Now(),
		}
		pool.jobsChannel <- job
	}

	// Aguardar processamento
	success := waitForCondition(t, 5*time.Second, func() bool {
		return dispatcher.GetCallCount() >= numJobs
	})

	assert.True(t, success, "Should process all jobs within timeout")

	calls := dispatcher.GetCalls()
	assert.Len(t, calls, numJobs)
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := NewMockDispatcher()
	logger := newTestLogger()

	pool := newWorkerPool(ctx, 2, 10, dispatcher, logger)

	// Enviar alguns jobs
	for i := 0; i < 5; i++ {
		job := &workerJob{
			line:       fmt.Sprintf("log line %d", i),
			sourcePath: "/tmp/test.log",
			timestamp:  time.Now(),
		}
		pool.jobsChannel <- job
	}

	// Cancelar contexto
	cancel()

	// Fechar pool
	pool.close()

	// Pool deve encerrar graciosamente
	// Não deve causar panic ou deadlock
}

func TestWorkerPool_ErrorHandling(t *testing.T) {
	ctx := context.Background()
	dispatcher := NewMockDispatcher()
	dispatcher.SetHandleError(fmt.Errorf("simulated dispatcher error"))
	logger := newTestLogger()

	pool := newWorkerPool(ctx, 2, 10, dispatcher, logger)
	defer pool.close()

	// Enviar job que causará erro
	job := &workerJob{
		line:       "test log line",
		sourcePath: "/tmp/test.log",
		timestamp:  time.Now(),
	}

	pool.jobsChannel <- job

	// Aguardar processamento
	time.Sleep(500 * time.Millisecond)

	// Erro não deve causar panic - worker deve continuar funcionando
	// Enviar outro job
	dispatcher.SetHandleError(nil)
	job2 := &workerJob{
		line:       "test log line 2",
		sourcePath: "/tmp/test.log",
		timestamp:  time.Now(),
	}

	pool.jobsChannel <- job2

	time.Sleep(500 * time.Millisecond)

	// Segundo job deve ser processado
	calls := dispatcher.GetCalls()
	assert.GreaterOrEqual(t, len(calls), 1, "Should process successful job")
}

// ===================================================================================
// TAREFA 7: Benchmarks
// ===================================================================================

func BenchmarkWorkerPool_ProcessLogLine(b *testing.B) {
	ctx := context.Background()
	dispatcher := NewMockDispatcher()
	logger := newTestLogger()

	pool := newWorkerPool(ctx, 4, 1000, dispatcher, logger)
	defer pool.close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job := &workerJob{
			line:       fmt.Sprintf("benchmark line %d", i),
			sourcePath: "/tmp/bench.log",
			timestamp:  time.Now(),
		}
		pool.jobsChannel <- job
	}

	// Aguardar todos os jobs serem processados
	for dispatcher.GetCallCount() < b.N {
		time.Sleep(1 * time.Millisecond)
	}
}

func BenchmarkFileMonitor_ReadLines(b *testing.B) {
	testFile := createTestFile(&testing.T{})

	// Escrever linhas para benchmark
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf("benchmark line %d", i))
	}
	writeToFile(&testing.T{}, testFile, lines...)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
		SeekStrategy:     "beginning",
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		fm.Start(ctx)

		// Aguardar processamento
		for dispatcher.GetCallCount() < 1000 {
			time.Sleep(1 * time.Millisecond)
		}

		fm.Stop()
		dispatcher.Reset()
	}
}

// ===================================================================================
// Testes Adicionais de Edge Cases
// ===================================================================================

func TestFileMonitor_NonExistentFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test with non-existent file in short mode")
	}

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{"/tmp/non-existent-file-xyz123.log"},
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)

	// nxadm/tail library will wait for the file to appear, so Start doesn't immediately fail
	// This is actually expected behavior - tail -f also waits for files to appear
	require.NoError(t, err, "Start should succeed - tail will wait for file to appear")

	// Verify no logs are processed (file doesn't exist)
	time.Sleep(1 * time.Second)
	assert.Equal(t, 0, dispatcher.GetCallCount(), "Should not process any logs from non-existent file")
}

func TestFileMonitor_EmptyFile(t *testing.T) {
	testFile := createTestFile(t)
	// File is already empty from createTestFile

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
		SeekStrategy:     "beginning",
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	// Aguardar
	time.Sleep(1 * time.Second)

	// Should not process any lines
	callCount := dispatcher.GetCallCount()
	assert.Equal(t, 0, callCount, "Should not process lines from empty file")

	// Now write a line
	writeToFile(t, testFile, "new line after empty")

	// Should process the new line
	success := waitForCondition(t, 5*time.Second, func() bool {
		return dispatcher.GetCallCount() > 0
	})

	assert.True(t, success, "Should process line written to previously empty file")
}

func TestFileMonitor_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	testFile := createTestFile(t)

	// Escrever arquivo grande
	var lines []string
	for i := 0; i < 10000; i++ {
		lines = append(lines, fmt.Sprintf("large file line %d - with some additional content to make it bigger", i))
	}
	writeToFile(t, testFile, lines...)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
		SeekStrategy:     "beginning",
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	// Aguardar processamento de arquivo grande
	success := waitForCondition(t, 30*time.Second, func() bool {
		return dispatcher.GetCallCount() >= 10000
	})

	assert.True(t, success, "Should process all 10000 lines from large file")
}

func TestFileMonitor_MultipleFiles(t *testing.T) {
	file1 := createTestFile(t)
	file2 := createTestFile(t)
	file3 := createTestFile(t)

	writeToFile(t, file1, "file1 line1", "file1 line2")
	writeToFile(t, file2, "file2 line1", "file2 line2")
	writeToFile(t, file3, "file3 line1", "file3 line2")

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{file1, file2, file3},
		SeekStrategy:     "beginning",
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)
	defer fm.Stop()

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	// Aguardar processamento de todos os arquivos
	success := waitForCondition(t, 10*time.Second, func() bool {
		return dispatcher.GetCallCount() >= 6
	})

	assert.True(t, success, "Should process all 6 lines from 3 files")

	calls := dispatcher.GetCalls()
	assert.GreaterOrEqual(t, len(calls), 6)

	// Verificar que linhas de diferentes arquivos foram processadas
	sourceIDs := make(map[string]bool)
	for _, call := range calls {
		sourceIDs[call.SourceID] = true
	}

	assert.GreaterOrEqual(t, len(sourceIDs), 3, "Should process lines from at least 3 different files")
}

func TestFileMonitor_GracefulShutdownDuringProcessing(t *testing.T) {
	testFile := createTestFile(t)

	config := types.FileMonitorServiceConfig{
		WatchDirectories: []string{testFile},
		SeekStrategy:     "beginning",
	}

	dispatcher := NewMockDispatcher()
	taskManager := &MockTaskManager{}
	logger := newTestLogger()

	fm, err := NewFileMonitor(config, dispatcher, taskManager, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = fm.Start(ctx)
	require.NoError(t, err)

	// Escrever muitas linhas
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeToFile(t, testFile, lines...)

	// Aguardar um pouco
	time.Sleep(500 * time.Millisecond)

	// Stop durante processamento
	start := time.Now()
	err = fm.Stop()
	duration := time.Since(start)

	assert.NoError(t, err)
	assert.Less(t, duration, 15*time.Second, "Should stop within shutdown timeout")

	// Verificar que algumas linhas foram processadas
	callCount := dispatcher.GetCallCount()
	t.Logf("Processed %d lines before shutdown", callCount)
	assert.Greater(t, callCount, 0, "Should have processed some lines")
}
