package slo

import (
	"context"
	"fmt"
	"sync"
	"time"


	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

// SLIConfig defines a Service Level Indicator
type SLIConfig struct {
	Name        string  `yaml:"name"`
	Description string  `yaml:"description"`
	Query       string  `yaml:"query"`
	Target      float64 `yaml:"target"`
	Window      string  `yaml:"window"`
	AlertQuery  string  `yaml:"alert_query,omitempty"`
}

// SLOConfig defines a Service Level Objective
type SLOConfig struct {
	Name         string      `yaml:"name"`
	Description  string      `yaml:"description"`
	SLIs         []SLIConfig `yaml:"slis"`
	ErrorBudget  float64     `yaml:"error_budget"`
	Window       string      `yaml:"window"`
	AlertOnBreach bool       `yaml:"alert_on_breach"`
	Severity     string      `yaml:"severity"`
}

// SLOManager manages SLI/SLO monitoring
type SLOManager struct {
	config         SLOManagerConfig
	logger         *logrus.Logger
	prometheusAPI  v1.API
	slos           map[string]*SLO
	slis           map[string]*SLI
	mutex          sync.RWMutex
	alertManager   AlertManager
	isRunning      bool
	stopChan       chan struct{}
	wg             sync.WaitGroup
}

// SLOManagerConfig configures the SLO manager
type SLOManagerConfig struct {
	Enabled           bool        `yaml:"enabled"`
	PrometheusURL     string      `yaml:"prometheus_url"`
	EvaluationInterval time.Duration `yaml:"evaluation_interval"`
	SLOs              []SLOConfig `yaml:"slos"`
	AlertWebhook      string      `yaml:"alert_webhook"`
	RetentionPeriod   time.Duration `yaml:"retention_period"`
}

// SLO represents a Service Level Objective
type SLO struct {
	Config      SLOConfig
	SLIs        []*SLI
	ErrorBudget ErrorBudget
	Status      SLOStatus
	History     []SLOMeasurement
	LastUpdate  time.Time
}

// SLI represents a Service Level Indicator
type SLI struct {
	Config      SLIConfig
	CurrentValue float64
	Target      float64
	Status      SLIStatus
	History     []SLIMeasurement
	LastUpdate  time.Time
}

// ErrorBudget tracks error budget consumption
type ErrorBudget struct {
	Total     float64 `json:"total"`
	Consumed  float64 `json:"consumed"`
	Remaining float64 `json:"remaining"`
	BurnRate  float64 `json:"burn_rate"`
}

// SLOStatus represents the status of an SLO
type SLOStatus string

const (
	SLOStatusHealthy   SLOStatus = "healthy"
	SLOStatusWarning   SLOStatus = "warning"
	SLOStatusCritical  SLOStatus = "critical"
	SLOStatusBreached  SLOStatus = "breached"
)

// SLIStatus represents the status of an SLI
type SLIStatus string

const (
	SLIStatusHealthy   SLIStatus = "healthy"
	SLIStatusWarning   SLIStatus = "warning"
	SLIStatusCritical  SLIStatus = "critical"
)

// SLOMeasurement represents a point-in-time SLO measurement
type SLOMeasurement struct {
	Timestamp   time.Time   `json:"timestamp"`
	Value       float64     `json:"value"`
	Status      SLOStatus   `json:"status"`
	ErrorBudget ErrorBudget `json:"error_budget"`
}

// SLIMeasurement represents a point-in-time SLI measurement
type SLIMeasurement struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
	Target    float64   `json:"target"`
	Status    SLIStatus `json:"status"`
}

// Prometheus metrics for SLI/SLO
var (
	sliValue = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sli_value",
			Help: "Current value of Service Level Indicators",
		},
		[]string{"sli_name", "slo_name"},
	)

	sloCompliance = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "slo_compliance",
			Help: "SLO compliance percentage",
		},
		[]string{"slo_name"},
	)

	errorBudgetRemaining = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "error_budget_remaining",
			Help: "Remaining error budget percentage",
		},
		[]string{"slo_name"},
	)

	errorBudgetBurnRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "error_budget_burn_rate",
			Help: "Error budget burn rate",
		},
		[]string{"slo_name"},
	)

	sloBreaches = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "slo_breaches_total",
			Help: "Total number of SLO breaches",
		},
		[]string{"slo_name", "severity"},
	)
)

// DefaultSLOManagerConfig returns default configuration
func DefaultSLOManagerConfig() SLOManagerConfig {
	return SLOManagerConfig{
		Enabled:            false,
		PrometheusURL:      "http://localhost:9090",
		EvaluationInterval: 1 * time.Minute,
		RetentionPeriod:    30 * 24 * time.Hour, // 30 days
		SLOs: []SLOConfig{
			{
				Name:        "log_ingestion_availability",
				Description: "Log ingestion service availability",
				ErrorBudget: 0.1, // 99.9% availability
				Window:      "30d",
				SLIs: []SLIConfig{
					{
						Name:        "ingestion_success_rate",
						Description: "Percentage of logs successfully ingested",
						Query:       "rate(logs_processed_total[5m]) / rate(logs_received_total[5m]) * 100",
						Target:      99.9,
						Window:      "5m",
					},
				},
			},
			{
				Name:        "log_processing_performance",
				Description: "Log processing performance",
				ErrorBudget: 0.05, // 99.95% within SLA
				Window:      "7d",
				SLIs: []SLIConfig{
					{
						Name:        "processing_latency_p99",
						Description: "99th percentile processing latency",
						Query:       "histogram_quantile(0.99, processing_duration_seconds_bucket)",
						Target:      100, // 100ms
						Window:      "5m",
					},
					{
						Name:        "processing_latency_p95",
						Description: "95th percentile processing latency",
						Query:       "histogram_quantile(0.95, processing_duration_seconds_bucket)",
						Target:      50, // 50ms
						Window:      "5m",
					},
				},
			},
			{
				Name:        "sink_delivery_reliability",
				Description: "Sink delivery reliability",
				ErrorBudget: 0.01, // 99.99% delivery success
				Window:      "24h",
				SLIs: []SLIConfig{
					{
						Name:        "sink_success_rate",
						Description: "Percentage of successful sink deliveries",
						Query:       "rate(logs_sent_total{status=\"success\"}[5m]) / rate(logs_sent_total[5m]) * 100",
						Target:      99.99,
						Window:      "5m",
					},
				},
			},
		},
	}
}

// NewSLOManager creates a new SLO manager
func NewSLOManager(config SLOManagerConfig, logger *logrus.Logger) (*SLOManager, error) {
	if !config.Enabled {
		return &SLOManager{
			config: config,
			logger: logger,
		}, nil
	}

	// Create Prometheus client
	client, err := api.NewClient(api.Config{
		Address: config.PrometheusURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	manager := &SLOManager{
		config:        config,
		logger:        logger,
		prometheusAPI: v1.NewAPI(client),
		slos:          make(map[string]*SLO),
		slis:          make(map[string]*SLI),
		alertManager:  NewAlertManager(config.AlertWebhook, logger),
		stopChan:      make(chan struct{}),
	}

	// Initialize SLOs
	for _, sloConfig := range config.SLOs {
		if err := manager.AddSLO(sloConfig); err != nil {
			return nil, err
		}
	}

	return manager, nil
}

// Start begins SLO monitoring
func (sm *SLOManager) Start(ctx context.Context) error {
	if !sm.config.Enabled {
		sm.logger.Info("SLO monitoring disabled")
		return nil
	}

	sm.isRunning = true
	sm.logger.WithField("evaluation_interval", sm.config.EvaluationInterval).Info("Starting SLO monitoring")

	// Start evaluation loop
	sm.wg.Add(1)
	go sm.evaluationLoop()

	// Start cleanup loop
	sm.wg.Add(1)
	go sm.cleanupLoop()

	return nil
}

// Stop stops SLO monitoring
func (sm *SLOManager) Stop() error {
	if !sm.isRunning {
		return nil
	}

	sm.logger.Info("Stopping SLO monitoring")
	sm.isRunning = false
	close(sm.stopChan)
	sm.wg.Wait()

	return nil
}

// AddSLO adds a new SLO to monitoring
func (sm *SLOManager) AddSLO(config SLOConfig) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	slo := &SLO{
		Config:     config,
		SLIs:       make([]*SLI, 0, len(config.SLIs)),
		Status:     SLOStatusHealthy,
		History:    make([]SLOMeasurement, 0),
		LastUpdate: time.Now(),
	}

	// Initialize error budget
	slo.ErrorBudget = ErrorBudget{
		Total:     config.ErrorBudget,
		Consumed:  0,
		Remaining: config.ErrorBudget,
		BurnRate:  0,
	}

	// Add SLIs
	for _, sliConfig := range config.SLIs {
		sli := &SLI{
			Config:     sliConfig,
			Target:     sliConfig.Target,
			Status:     SLIStatusHealthy,
			History:    make([]SLIMeasurement, 0),
			LastUpdate: time.Now(),
		}

		slo.SLIs = append(slo.SLIs, sli)
		sm.slis[sliConfig.Name] = sli
	}

	sm.slos[config.Name] = slo
	sm.logger.WithField("slo_name", config.Name).Info("SLO added to monitoring")

	return nil
}

// evaluationLoop runs the SLO evaluation loop
func (sm *SLOManager) evaluationLoop() {
	defer sm.wg.Done()

	ticker := time.NewTicker(sm.config.EvaluationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopChan:
			return
		case <-ticker.C:
			sm.evaluateAll()
		}
	}
}

// evaluateAll evaluates all SLOs
func (sm *SLOManager) evaluateAll() {
	sm.mutex.RLock()
	slos := make([]*SLO, 0, len(sm.slos))
	for _, slo := range sm.slos {
		slos = append(slos, slo)
	}
	sm.mutex.RUnlock()

	for _, slo := range slos {
		if err := sm.evaluateSLO(slo); err != nil {
			sm.logger.WithError(err).WithField("slo_name", slo.Config.Name).Error("Failed to evaluate SLO")
		}
	}
}

// evaluateSLO evaluates a single SLO
func (sm *SLOManager) evaluateSLO(slo *SLO) error {
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Evaluate each SLI
	var totalCompliance float64
	var breachedSLIs int

	for _, sli := range slo.SLIs {
		if err := sm.evaluateSLI(ctx, sli); err != nil {
			sm.logger.WithError(err).WithField("sli_name", sli.Config.Name).Error("Failed to evaluate SLI")
			continue
		}

		// Calculate compliance
		compliance := (sli.CurrentValue / sli.Target) * 100
		if compliance > 100 {
			compliance = 100
		}

		totalCompliance += compliance

		// Check for breach
		if sli.CurrentValue < sli.Target {
			breachedSLIs++
			if sli.Status != SLIStatusCritical {
				sli.Status = SLIStatusCritical
				sm.alertManager.SendSLIAlert(sli, "SLI breach detected")
			}
		} else {
			sli.Status = SLIStatusHealthy
		}

		// Update metrics
		sliValue.WithLabelValues(sli.Config.Name, slo.Config.Name).Set(sli.CurrentValue)
	}

	// Calculate overall SLO status
	if len(slo.SLIs) > 0 {
		avgCompliance := totalCompliance / float64(len(slo.SLIs))

		// Update error budget
		sm.updateErrorBudget(slo, avgCompliance)

		// Determine SLO status
		previousStatus := slo.Status
		slo.Status = sm.calculateSLOStatus(slo, avgCompliance, breachedSLIs)

		// Alert on status change
		if slo.Status != previousStatus && slo.Config.AlertOnBreach {
			sm.alertManager.SendSLOAlert(slo, fmt.Sprintf("SLO status changed from %s to %s", previousStatus, slo.Status))

			// Record breach metric
			if slo.Status == SLOStatusBreached {
				sloBreaches.WithLabelValues(slo.Config.Name, slo.Config.Severity).Inc()
			}
		}

		// Record measurement
		measurement := SLOMeasurement{
			Timestamp:   now,
			Value:       avgCompliance,
			Status:      slo.Status,
			ErrorBudget: slo.ErrorBudget,
		}

		slo.History = append(slo.History, measurement)
		slo.LastUpdate = now

		// Update metrics
		sloCompliance.WithLabelValues(slo.Config.Name).Set(avgCompliance)
		errorBudgetRemaining.WithLabelValues(slo.Config.Name).Set(slo.ErrorBudget.Remaining)
		errorBudgetBurnRate.WithLabelValues(slo.Config.Name).Set(slo.ErrorBudget.BurnRate)
	}

	return nil
}

// evaluateSLI evaluates a single SLI
func (sm *SLOManager) evaluateSLI(ctx context.Context, sli *SLI) error {
	// Query Prometheus
	result, warnings, err := sm.prometheusAPI.Query(ctx, sli.Config.Query, time.Now())
	if err != nil {
		return fmt.Errorf("failed to query Prometheus: %w", err)
	}

	if len(warnings) > 0 {
		sm.logger.WithField("warnings", warnings).Warn("Prometheus query warnings")
	}

	// Parse result
	value, err := sm.parsePrometheusResult(result)
	if err != nil {
		return err
	}

	// Update SLI
	sli.CurrentValue = value
	sli.LastUpdate = time.Now()

	// Record measurement
	measurement := SLIMeasurement{
		Timestamp:   time.Now().UTC(),
		Value:     value,
		Target:    sli.Target,
		Status:    sli.Status,
	}

	sli.History = append(sli.History, measurement)

	return nil
}

// parsePrometheusResult parses Prometheus query result
func (sm *SLOManager) parsePrometheusResult(result interface{}) (float64, error) {
	// Implementation depends on Prometheus result format
	// This is a simplified version
	return 0.0, nil
}

// updateErrorBudget updates the error budget for an SLO
func (sm *SLOManager) updateErrorBudget(slo *SLO, compliance float64) {
	// Calculate burn rate based on compliance
	if compliance < 100 {
		errorRate := (100 - compliance) / 100
		slo.ErrorBudget.BurnRate = errorRate

		// Update consumed budget (simplified calculation)
		window, _ := time.ParseDuration(slo.Config.Window)
		intervalFraction := sm.config.EvaluationInterval.Seconds() / window.Seconds()
		consumption := errorRate * intervalFraction

		slo.ErrorBudget.Consumed += consumption
		slo.ErrorBudget.Remaining = slo.ErrorBudget.Total - slo.ErrorBudget.Consumed

		if slo.ErrorBudget.Remaining < 0 {
			slo.ErrorBudget.Remaining = 0
		}
	} else {
		slo.ErrorBudget.BurnRate = 0
	}
}

// calculateSLOStatus determines SLO status based on compliance and error budget
func (sm *SLOManager) calculateSLOStatus(slo *SLO, compliance float64, breachedSLIs int) SLOStatus {
	// Check error budget
	budgetUsed := (slo.ErrorBudget.Consumed / slo.ErrorBudget.Total) * 100

	if budgetUsed >= 100 {
		return SLOStatusBreached
	}

	if budgetUsed >= 80 {
		return SLOStatusCritical
	}

	if budgetUsed >= 60 || breachedSLIs > 0 {
		return SLOStatusWarning
	}

	return SLOStatusHealthy
}

// cleanupLoop removes old measurements
func (sm *SLOManager) cleanupLoop() {
	defer sm.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopChan:
			return
		case <-ticker.C:
			sm.cleanupOldMeasurements()
		}
	}
}

// cleanupOldMeasurements removes measurements older than retention period
func (sm *SLOManager) cleanupOldMeasurements() {
	cutoff := time.Now().Add(-sm.config.RetentionPeriod)

	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for _, slo := range sm.slos {
		// Clean SLO history
		var newHistory []SLOMeasurement
		for _, measurement := range slo.History {
			if measurement.Timestamp.After(cutoff) {
				newHistory = append(newHistory, measurement)
			}
		}
		slo.History = newHistory

		// Clean SLI history
		for _, sli := range slo.SLIs {
			var newSLIHistory []SLIMeasurement
			for _, measurement := range sli.History {
				if measurement.Timestamp.After(cutoff) {
					newSLIHistory = append(newSLIHistory, measurement)
				}
			}
			sli.History = newSLIHistory
		}
	}
}

// GetSLOStatus returns the current status of all SLOs
func (sm *SLOManager) GetSLOStatus() map[string]*SLO {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	result := make(map[string]*SLO)
	for name, slo := range sm.slos {
		result[name] = slo
	}

	return result
}

// GetSLIStatus returns the current status of all SLIs
func (sm *SLOManager) GetSLIStatus() map[string]*SLI {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	result := make(map[string]*SLI)
	for name, sli := range sm.slis {
		result[name] = sli
	}

	return result
}

// AlertManager handles SLO/SLI alerts
type AlertManager struct {
	webhookURL string
	logger     *logrus.Logger
}

// NewAlertManager creates a new alert manager
func NewAlertManager(webhookURL string, logger *logrus.Logger) AlertManager {
	return AlertManager{
		webhookURL: webhookURL,
		logger:     logger,
	}
}

// SendSLOAlert sends an SLO alert
func (am AlertManager) SendSLOAlert(slo *SLO, message string) {
	am.logger.WithFields(logrus.Fields{
		"slo_name": slo.Config.Name,
		"status":   slo.Status,
		"message":  message,
	}).Warn("SLO alert")

	// Send webhook if configured
	// Implementation would send HTTP POST to webhook URL
}

// SendSLIAlert sends an SLI alert
func (am AlertManager) SendSLIAlert(sli *SLI, message string) {
	am.logger.WithFields(logrus.Fields{
		"sli_name": sli.Config.Name,
		"value":    sli.CurrentValue,
		"target":   sli.Target,
		"status":   sli.Status,
		"message":  message,
	}).Warn("SLI alert")

	// Send webhook if configured
	// Implementation would send HTTP POST to webhook URL
}