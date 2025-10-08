package throttling

import (
	"context"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// AdaptiveThrottler implementa throttling adaptativo baseado em carga do sistema
type AdaptiveThrottler struct {
	config Config
	logger *logrus.Logger

	// Estado atual
	currentSleepBase time.Duration
	cpuHistory       *MetricWindow
	memoryHistory    *MetricWindow
	queueHistory     *MetricWindow

	// Estatísticas
	stats Stats
	mutex sync.RWMutex

	// Controle
	ctx    context.Context
	cancel context.CancelFunc
}

// Config configuração do throttler adaptativo
type Config struct {
	// Habilitar throttling
	Enabled bool `yaml:"enabled"`

	// Sleep base inicial
	SleepBase time.Duration `yaml:"sleep_base"`

	// Sleep mínimo
	SleepMin time.Duration `yaml:"sleep_min"`

	// Sleep máximo
	SleepMax time.Duration `yaml:"sleep_max"`

	// Thresholds de CPU
	CPULowThreshold  float64 `yaml:"cpu_low_threshold"`   // < threshold: reduzir sleep
	CPUHighThreshold float64 `yaml:"cpu_high_threshold"`  // > threshold: aumentar sleep

	// Thresholds de memória
	MemoryLowThreshold  float64 `yaml:"memory_low_threshold"`
	MemoryHighThreshold float64 `yaml:"memory_high_threshold"`

	// Thresholds de fila
	QueueLowThreshold  int `yaml:"queue_low_threshold"`
	QueueHighThreshold int `yaml:"queue_high_threshold"`

	// Intervalo de monitoramento
	MonitoringInterval time.Duration `yaml:"monitoring_interval"`

	// Janela de histórico
	HistoryWindowSize int `yaml:"history_window_size"`

	// Fator de adaptação
	AdaptationFactor float64 `yaml:"adaptation_factor"`

	// Fator de suavização
	SmoothingFactor float64 `yaml:"smoothing_factor"`

	// Habilitar diferentes tipos de monitoramento
	MonitorCPU    bool `yaml:"monitor_cpu"`
	MonitorMemory bool `yaml:"monitor_memory"`
	MonitorQueue  bool `yaml:"monitor_queue"`

	// Pesos para diferentes métricas
	CPUWeight    float64 `yaml:"cpu_weight"`
	MemoryWeight float64 `yaml:"memory_weight"`
	QueueWeight  float64 `yaml:"queue_weight"`
}

// Stats estatísticas do throttler
type Stats struct {
	TotalThrottles     int64         `json:"total_throttles"`
	TotalSleepTime     time.Duration `json:"total_sleep_time"`
	CurrentSleepBase   time.Duration `json:"current_sleep_base"`
	AdaptationCount    int64         `json:"adaptation_count"`
	LastAdaptation     time.Time     `json:"last_adaptation"`
	AvgCPUPercent      float64       `json:"avg_cpu_percent"`
	AvgMemoryPercent   float64       `json:"avg_memory_percent"`
	AvgQueueSize       float64       `json:"avg_queue_size"`
	LoadScore          float64       `json:"load_score"`
}

// MetricWindow janela deslizante para métricas
type MetricWindow struct {
	values []float64
	index  int
	size   int
	mutex  sync.Mutex
}

// LoadInfo informações de carga do sistema
type LoadInfo struct {
	CPUPercent    float64
	MemoryPercent float64
	QueueSize     int
	LoadScore     float64
}

// NewMetricWindow cria nova janela de métricas
func NewMetricWindow(size int) *MetricWindow {
	return &MetricWindow{
		values: make([]float64, size),
		size:   size,
	}
}

// Add adiciona valor à janela
func (mw *MetricWindow) Add(value float64) {
	mw.mutex.Lock()
	defer mw.mutex.Unlock()

	mw.values[mw.index] = value
	mw.index = (mw.index + 1) % mw.size
}

// Average calcula média dos valores
func (mw *MetricWindow) Average() float64 {
	mw.mutex.Lock()
	defer mw.mutex.Unlock()

	var total float64
	count := 0

	for _, value := range mw.values {
		if value > 0 {
			total += value
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return total / float64(count)
}

// NewAdaptiveThrottler cria novo throttler adaptativo
func NewAdaptiveThrottler(config Config, logger *logrus.Logger) *AdaptiveThrottler {
	ctx, cancel := context.WithCancel(context.Background())

	// Valores padrão
	if config.SleepBase == 0 {
		config.SleepBase = 100 * time.Millisecond
	}
	if config.SleepMin == 0 {
		config.SleepMin = 10 * time.Millisecond
	}
	if config.SleepMax == 0 {
		config.SleepMax = 5 * time.Second
	}
	if config.CPULowThreshold == 0 {
		config.CPULowThreshold = 30
	}
	if config.CPUHighThreshold == 0 {
		config.CPUHighThreshold = 80
	}
	if config.MemoryLowThreshold == 0 {
		config.MemoryLowThreshold = 60
	}
	if config.MemoryHighThreshold == 0 {
		config.MemoryHighThreshold = 85
	}
	if config.QueueLowThreshold == 0 {
		config.QueueLowThreshold = 100
	}
	if config.QueueHighThreshold == 0 {
		config.QueueHighThreshold = 1000
	}
	if config.MonitoringInterval == 0 {
		config.MonitoringInterval = 5 * time.Second
	}
	if config.HistoryWindowSize == 0 {
		config.HistoryWindowSize = 20
	}
	if config.AdaptationFactor == 0 {
		config.AdaptationFactor = 0.2
	}
	if config.SmoothingFactor == 0 {
		config.SmoothingFactor = 0.7
	}
	if config.CPUWeight == 0 {
		config.CPUWeight = 0.5
	}
	if config.MemoryWeight == 0 {
		config.MemoryWeight = 0.3
	}
	if config.QueueWeight == 0 {
		config.QueueWeight = 0.2
	}

	// Habilitar monitoramento por padrão
	if !config.MonitorCPU && !config.MonitorMemory && !config.MonitorQueue {
		config.MonitorCPU = true
		config.MonitorMemory = true
		config.MonitorQueue = true
	}

	at := &AdaptiveThrottler{
		config:           config,
		logger:           logger,
		currentSleepBase: config.SleepBase,
		cpuHistory:       NewMetricWindow(config.HistoryWindowSize),
		memoryHistory:    NewMetricWindow(config.HistoryWindowSize),
		queueHistory:     NewMetricWindow(config.HistoryWindowSize),
		ctx:              ctx,
		cancel:           cancel,
	}

	// Iniciar monitoramento
	go at.monitoringLoop()

	return at
}

// Throttle executa throttling baseado na carga atual
func (at *AdaptiveThrottler) Throttle(ctx context.Context) error {
	if !at.config.Enabled {
		return nil
	}

	at.mutex.Lock()
	sleepDuration := at.currentSleepBase
	at.stats.TotalThrottles++
	at.stats.TotalSleepTime += sleepDuration
	at.mutex.Unlock()

	if sleepDuration > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDuration):
			return nil
		}
	}

	return nil
}

// ThrottleWithQueueSize executa throttling considerando tamanho da fila
func (at *AdaptiveThrottler) ThrottleWithQueueSize(ctx context.Context, queueSize int) error {
	if !at.config.Enabled {
		return nil
	}

	// Adicionar tamanho da fila ao histórico
	if at.config.MonitorQueue {
		at.queueHistory.Add(float64(queueSize))
	}

	// Calcular sleep adicional baseado na fila
	queueMultiplier := at.calculateQueueMultiplier(queueSize)

	at.mutex.Lock()
	baseSleep := at.currentSleepBase
	adjustedSleep := time.Duration(float64(baseSleep) * queueMultiplier)

	// Aplicar limites
	if adjustedSleep < at.config.SleepMin {
		adjustedSleep = at.config.SleepMin
	}
	if adjustedSleep > at.config.SleepMax {
		adjustedSleep = at.config.SleepMax
	}

	at.stats.TotalThrottles++
	at.stats.TotalSleepTime += adjustedSleep
	at.mutex.Unlock()

	if adjustedSleep > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(adjustedSleep):
			return nil
		}
	}

	return nil
}

// calculateQueueMultiplier calcula multiplicador baseado no tamanho da fila
func (at *AdaptiveThrottler) calculateQueueMultiplier(queueSize int) float64 {
	if queueSize <= at.config.QueueLowThreshold {
		return 0.5 // Reduzir throttling
	}

	if queueSize >= at.config.QueueHighThreshold {
		return 3.0 // Aumentar throttling significativamente
	}

	// Interpolação linear entre os thresholds
	ratio := float64(queueSize-at.config.QueueLowThreshold) /
		     float64(at.config.QueueHighThreshold-at.config.QueueLowThreshold)
	return 0.5 + ratio*2.5 // de 0.5 a 3.0
}

// monitoringLoop loop de monitoramento de sistema
func (at *AdaptiveThrottler) monitoringLoop() {
	ticker := time.NewTicker(at.config.MonitoringInterval)
	defer ticker.Stop()

	for {
		select {
		case <-at.ctx.Done():
			return
		case <-ticker.C:
			at.collectMetrics()
			at.adaptThrottling()
		}
	}
}

// collectMetrics coleta métricas do sistema
func (at *AdaptiveThrottler) collectMetrics() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// CPU (aproximação simples baseada em goroutines)
	if at.config.MonitorCPU {
		cpuPercent := at.estimateCPUUsage()
		at.cpuHistory.Add(cpuPercent)
	}

	// Memória
	if at.config.MonitorMemory {
		memoryPercent := at.calculateMemoryUsage(&memStats)
		at.memoryHistory.Add(memoryPercent)
	}
}

// estimateCPUUsage estima uso de CPU (método simplificado)
func (at *AdaptiveThrottler) estimateCPUUsage() float64 {
	// Método simplificado: número de goroutines vs CPUs
	numGoroutines := runtime.NumGoroutine()
	numCPU := runtime.NumCPU()

	// Estimativa básica
	cpuEstimate := float64(numGoroutines) / float64(numCPU) * 10

	// Limitar a 100%
	if cpuEstimate > 100 {
		cpuEstimate = 100
	}

	return cpuEstimate
}

// calculateMemoryUsage calcula percentual de uso de memória
func (at *AdaptiveThrottler) calculateMemoryUsage(memStats *runtime.MemStats) float64 {
	// Memória heap em uso vs alocada
	heapInUse := float64(memStats.HeapInuse)
	heapSys := float64(memStats.HeapSys)

	if heapSys == 0 {
		return 0
	}

	return (heapInUse / heapSys) * 100
}

// adaptThrottling adapta throttling baseado nas métricas
func (at *AdaptiveThrottler) adaptThrottling() {
	loadInfo := at.calculateLoadScore()

	at.mutex.Lock()
	defer at.mutex.Unlock()

	oldSleep := at.currentSleepBase
	newSleep := at.calculateNewSleep(loadInfo)

	// Aplicar suavização
	if at.stats.AdaptationCount > 0 {
		newSleep = time.Duration(
			float64(oldSleep)*at.config.SmoothingFactor +
			float64(newSleep)*(1-at.config.SmoothingFactor))
	}

	// Aplicar limites
	if newSleep < at.config.SleepMin {
		newSleep = at.config.SleepMin
	}
	if newSleep > at.config.SleepMax {
		newSleep = at.config.SleepMax
	}

	at.currentSleepBase = newSleep
	at.stats.AdaptationCount++
	at.stats.LastAdaptation = time.Now()
	at.stats.CurrentSleepBase = newSleep
	at.stats.AvgCPUPercent = loadInfo.CPUPercent
	at.stats.AvgMemoryPercent = loadInfo.MemoryPercent
	at.stats.AvgQueueSize = float64(loadInfo.QueueSize)
	at.stats.LoadScore = loadInfo.LoadScore

	// Log se mudança significativa
	changePercent := math.Abs(float64(newSleep-oldSleep)) / float64(oldSleep) * 100
	if changePercent > 10 {
		at.logger.WithFields(logrus.Fields{
			"old_sleep_ms":    oldSleep.Milliseconds(),
			"new_sleep_ms":    newSleep.Milliseconds(),
			"load_score":      loadInfo.LoadScore,
			"cpu_percent":     loadInfo.CPUPercent,
			"memory_percent":  loadInfo.MemoryPercent,
			"queue_size":      loadInfo.QueueSize,
		}).Info("Throttling adapted")
	}
}

// calculateLoadScore calcula score de carga composto
func (at *AdaptiveThrottler) calculateLoadScore() *LoadInfo {
	cpuPercent := at.cpuHistory.Average()
	memoryPercent := at.memoryHistory.Average()
	queueSize := at.queueHistory.Average()

	// Normalizar métricas (0-1)
	var cpuScore, memoryScore, queueScore float64

	if at.config.MonitorCPU && cpuPercent > 0 {
		cpuScore = cpuPercent / 100
	}

	if at.config.MonitorMemory && memoryPercent > 0 {
		memoryScore = memoryPercent / 100
	}

	if at.config.MonitorQueue && queueSize > 0 {
		queueScore = math.Min(queueSize/float64(at.config.QueueHighThreshold), 1.0)
	}

	// Calcular score ponderado
	loadScore := cpuScore*at.config.CPUWeight +
		        memoryScore*at.config.MemoryWeight +
		        queueScore*at.config.QueueWeight

	return &LoadInfo{
		CPUPercent:    cpuPercent,
		MemoryPercent: memoryPercent,
		QueueSize:     int(queueSize),
		LoadScore:     loadScore,
	}
}

// calculateNewSleep calcula novo tempo de sleep baseado na carga
func (at *AdaptiveThrottler) calculateNewSleep(loadInfo *LoadInfo) time.Duration {
	loadScore := loadInfo.LoadScore

	if loadScore < 0.3 {
		// Carga baixa - reduzir throttling
		reduction := 1 - at.config.AdaptationFactor
		return time.Duration(float64(at.currentSleepBase) * reduction)
	} else if loadScore > 0.7 {
		// Carga alta - aumentar throttling
		increase := 1 + at.config.AdaptationFactor*2
		return time.Duration(float64(at.currentSleepBase) * increase)
	}

	// Carga média - pequenos ajustes
	if loadScore > 0.5 {
		increase := 1 + at.config.AdaptationFactor*0.5
		return time.Duration(float64(at.currentSleepBase) * increase)
	} else {
		reduction := 1 - at.config.AdaptationFactor*0.5
		return time.Duration(float64(at.currentSleepBase) * reduction)
	}
}

// GetCurrentSleep retorna tempo de sleep atual
func (at *AdaptiveThrottler) GetCurrentSleep() time.Duration {
	at.mutex.RLock()
	defer at.mutex.RUnlock()
	return at.currentSleepBase
}

// GetLoadInfo retorna informações atuais de carga
func (at *AdaptiveThrottler) GetLoadInfo() *LoadInfo {
	return at.calculateLoadScore()
}

// GetStats retorna estatísticas
func (at *AdaptiveThrottler) GetStats() Stats {
	at.mutex.RLock()
	defer at.mutex.RUnlock()
	return at.stats
}

// GetInfo retorna informações detalhadas
func (at *AdaptiveThrottler) GetInfo() map[string]interface{} {
	stats := at.GetStats()
	loadInfo := at.GetLoadInfo()

	throttleRate := float64(0)
	if stats.TotalThrottles > 0 {
		throttleRate = float64(stats.TotalSleepTime.Milliseconds()) / float64(stats.TotalThrottles)
	}

	return map[string]interface{}{
		"enabled":                 at.config.Enabled,
		"current_sleep_base_ms":   stats.CurrentSleepBase.Milliseconds(),
		"sleep_min_ms":            at.config.SleepMin.Milliseconds(),
		"sleep_max_ms":            at.config.SleepMax.Milliseconds(),
		"cpu_low_threshold":       at.config.CPULowThreshold,
		"cpu_high_threshold":      at.config.CPUHighThreshold,
		"memory_low_threshold":    at.config.MemoryLowThreshold,
		"memory_high_threshold":   at.config.MemoryHighThreshold,
		"queue_low_threshold":     at.config.QueueLowThreshold,
		"queue_high_threshold":    at.config.QueueHighThreshold,
		"total_throttles":         stats.TotalThrottles,
		"total_sleep_time_ms":     stats.TotalSleepTime.Milliseconds(),
		"adaptation_count":        stats.AdaptationCount,
		"last_adaptation":         stats.LastAdaptation,
		"avg_cpu_percent":         stats.AvgCPUPercent,
		"avg_memory_percent":      stats.AvgMemoryPercent,
		"avg_queue_size":          stats.AvgQueueSize,
		"load_score":              stats.LoadScore,
		"avg_throttle_ms":         throttleRate,
		"current_load":            loadInfo,
	}
}

// Reset reseta o throttler para configuração inicial
func (at *AdaptiveThrottler) Reset() {
	at.mutex.Lock()
	defer at.mutex.Unlock()

	at.currentSleepBase = at.config.SleepBase
	at.stats = Stats{}
	at.cpuHistory = NewMetricWindow(at.config.HistoryWindowSize)
	at.memoryHistory = NewMetricWindow(at.config.HistoryWindowSize)
	at.queueHistory = NewMetricWindow(at.config.HistoryWindowSize)

	at.logger.Info("Adaptive throttler reset")
}

// Stop para o throttler
func (at *AdaptiveThrottler) Stop() {
	at.cancel()
}