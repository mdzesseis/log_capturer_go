package anomaly

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// AnomalyDetector detecta anomalias em logs usando técnicas de ML
type AnomalyDetector struct {
	config    Config
	logger    *logrus.Logger
	models    map[string]Model
	modelsMux sync.RWMutex

	// Feature extractors
	extractors map[string]FeatureExtractor

	// Training data buffer
	trainingBuffer   []ProcessedLogEntry
	trainingMux      sync.RWMutex
	lastTrainingTime time.Time

	// Detection stats
	stats Stats

	// C2: Context Leak Fix - Add cancel function for proper shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config configuração do detector de anomalias
type Config struct {
	Enabled               bool              `yaml:"enabled"`
	Algorithm             string            `yaml:"algorithm"`              // "isolation_forest", "statistical", "ml_ensemble"
	SensitivityThreshold  float64           `yaml:"sensitivity_threshold"`  // 0.0 - 1.0
	WindowSize            string            `yaml:"window_size"`            // "1h", "24h", etc.
	TrainingInterval      string            `yaml:"training_interval"`      // "1h", "24h", etc.
	MinTrainingSamples    int               `yaml:"min_training_samples"`   // Minimum samples for training
	MaxTrainingSamples    int               `yaml:"max_training_samples"`   // Maximum samples to keep
	Features              []string          `yaml:"features"`               // Features to extract
	ModelConfig           map[string]interface{} `yaml:"model_config"`      // Algorithm-specific config
	WhitelistPatterns     []string          `yaml:"whitelist_patterns"`     // Patterns to ignore
	BlacklistPatterns     []string          `yaml:"blacklist_patterns"`     // Patterns to always flag
	AutoTuning            bool              `yaml:"auto_tuning"`            // Enable auto-tuning
	AlertThreshold        float64           `yaml:"alert_threshold"`        // Threshold for alerts
	SaveModel             bool              `yaml:"save_model"`             // Save trained models
	ModelPath             string            `yaml:"model_path"`             // Path to save models
}

// ProcessedLogEntry representa um log processado para ML
type ProcessedLogEntry struct {
	Timestamp    time.Time         `json:"timestamp"`
	SourceType   string            `json:"source_type"`
	SourceID     string            `json:"source_id"`
	Message      string            `json:"message"`
	Level        string            `json:"level"`
	Features     map[string]float64 `json:"features"`
	Labels       map[string]string `json:"labels"`
	AnomalyScore float64           `json:"anomaly_score"`
	IsAnomaly    bool              `json:"is_anomaly"`
	ModelUsed    string            `json:"model_used"`
}

// AnomalyResult resultado da detecção de anomalia
type AnomalyResult struct {
	IsAnomaly        bool               `json:"is_anomaly"`
	AnomalyScore     float64            `json:"anomaly_score"`
	Confidence       float64            `json:"confidence"`
	Features         map[string]float64 `json:"features"`
	ModelUsed        string             `json:"model_used"`
	Reason           string             `json:"reason"`
	Severity         string             `json:"severity"` // "low", "medium", "high", "critical"
	Recommendations  []string           `json:"recommendations"`
	SimilarEntries   []string           `json:"similar_entries"`
}

// Stats estatísticas do detector
type Stats struct {
	TotalProcessed    int64     `json:"total_processed"`
	AnomaliesDetected int64     `json:"anomalies_detected"`
	FalsePositives    int64     `json:"false_positives"`
	TruePositives     int64     `json:"true_positives"`
	ModelAccuracy     float64   `json:"model_accuracy"`
	LastTrainingTime  time.Time `json:"last_training_time"`
	TrainingSamples   int       `json:"training_samples"`
	ModelsActive      int       `json:"models_active"`
	AverageScore      float64   `json:"average_score"`
	LastAnomalyTime   time.Time `json:"last_anomaly_time"`
}

// Model interface para modelos de ML
type Model interface {
	Train(data []ProcessedLogEntry) error
	Predict(entry ProcessedLogEntry) (float64, error)
	GetType() string
	GetAccuracy() float64
	Save(path string) error
	Load(path string) error
}

// FeatureExtractor interface para extração de features
type FeatureExtractor interface {
	Extract(entry *types.LogEntry) (map[string]float64, error)
	GetFeatureNames() []string
}

// NewAnomalyDetector cria uma nova instância do detector
func NewAnomalyDetector(config Config, logger *logrus.Logger) (*AnomalyDetector, error) {
	// C2: Always create cancelable context even when disabled
	ctx, cancel := context.WithCancel(context.Background())

	if !config.Enabled {
		return &AnomalyDetector{
			config: config,
			logger: logger,
			ctx:    ctx,
			cancel: cancel,
		}, nil
	}

	// Set defaults
	if config.SensitivityThreshold == 0 {
		config.SensitivityThreshold = 0.7
	}
	if config.MinTrainingSamples == 0 {
		config.MinTrainingSamples = 100
	}
	if config.MaxTrainingSamples == 0 {
		config.MaxTrainingSamples = 10000
	}
	if config.AlertThreshold == 0 {
		config.AlertThreshold = 0.8
	}

	detector := &AnomalyDetector{
		config:     config,
		logger:     logger,
		models:     make(map[string]Model),
		extractors: make(map[string]FeatureExtractor),
		ctx:        ctx,
		cancel:     cancel,
	}

	// Initialize feature extractors
	if err := detector.initializeExtractors(); err != nil {
		return nil, fmt.Errorf("failed to initialize extractors: %w", err)
	}

	// Initialize models
	if err := detector.initializeModels(); err != nil {
		return nil, fmt.Errorf("failed to initialize models: %w", err)
	}

	return detector, nil
}

// initializeExtractors inicializa os extratores de features
func (ad *AnomalyDetector) initializeExtractors() error {
	// Text features extractor - use constructor to initialize regex patterns
	ad.extractors["text"] = NewTextFeatureExtractor()

	// Statistical features extractor - use constructor to initialize maps
	ad.extractors["statistical"] = NewStatisticalFeatureExtractor()

	// Temporal features extractor - use constructor to initialize slices
	ad.extractors["temporal"] = NewTemporalFeatureExtractor()

	// Pattern features extractor - use constructor to compile regex patterns
	ad.extractors["pattern"] = NewPatternFeatureExtractor()

	return nil
}

// initializeModels inicializa os modelos de ML
func (ad *AnomalyDetector) initializeModels() error {
	switch ad.config.Algorithm {
	case "isolation_forest":
		// Use constructor to initialize default parameters
		model := NewIsolationForestModel()
		model.config = ad.config.ModelConfig
		model.logger = ad.logger
		ad.models["main"] = model
	case "statistical":
		// Use constructor to initialize maps
		model := NewStatisticalModel()
		model.config = ad.config.ModelConfig
		model.logger = ad.logger
		ad.models["main"] = model
	case "ml_ensemble":
		// Multiple models for ensemble - use constructors for each
		isolationModel := NewIsolationForestModel()
		isolationModel.config = ad.config.ModelConfig
		isolationModel.logger = ad.logger
		ad.models["isolation"] = isolationModel

		statisticalModel := NewStatisticalModel()
		statisticalModel.config = ad.config.ModelConfig
		statisticalModel.logger = ad.logger
		ad.models["statistical"] = statisticalModel

		neuralModel := NewNeuralNetworkModel()
		neuralModel.config = ad.config.ModelConfig
		neuralModel.logger = ad.logger
		ad.models["neural"] = neuralModel
	default:
		return fmt.Errorf("unsupported algorithm: %s", ad.config.Algorithm)
	}

	ad.stats.ModelsActive = len(ad.models)
	return nil
}

// Start inicia o detector de anomalias
func (ad *AnomalyDetector) Start() error {
	if !ad.config.Enabled {
		ad.logger.Info("Anomaly detector disabled")
		return nil
	}

	ad.logger.Info("Starting anomaly detector")

	// Load existing models if available
	if ad.config.SaveModel && ad.config.ModelPath != "" {
		if err := ad.loadModels(); err != nil {
			ad.logger.WithError(err).Warn("Failed to load existing models, will train new ones")
		}
	}

	// Start periodic training
	ad.wg.Add(1)
	go ad.periodicTraining()

	ad.logger.WithFields(logrus.Fields{
		"algorithm":          ad.config.Algorithm,
		"sensitivity":        ad.config.SensitivityThreshold,
		"training_interval":  ad.config.TrainingInterval,
		"models_active":      ad.stats.ModelsActive,
	}).Info("Anomaly detector started")

	return nil
}

// Stop para o detector
func (ad *AnomalyDetector) Stop() error {
	if !ad.config.Enabled {
		return nil
	}

	ad.logger.Info("Stopping anomaly detector")

	// C2: Cancel context to signal goroutines to stop
	if ad.cancel != nil {
		ad.cancel()
	}

	// Wait for goroutines to finish (with timeout for safety)
	done := make(chan struct{})
	go func() {
		ad.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		ad.logger.Info("All anomaly detector goroutines stopped")
	case <-time.After(5 * time.Second):
		ad.logger.Warn("Timeout waiting for anomaly detector goroutines to stop")
	}

	// Save models if configured
	if ad.config.SaveModel && ad.config.ModelPath != "" {
		if err := ad.saveModels(); err != nil {
			ad.logger.WithError(err).Error("Failed to save models")
		}
	}

	ad.logger.Info("Anomaly detector stopped")
	return nil
}

// DetectAnomaly detecta anomalias em um log entry
func (ad *AnomalyDetector) DetectAnomaly(entry *types.LogEntry) (*AnomalyResult, error) {
	if !ad.config.Enabled {
		return &AnomalyResult{
			IsAnomaly: false,
			Reason:    "detector disabled",
		}, nil
	}

	ad.stats.TotalProcessed++

	// Extract features
	features, err := ad.extractFeatures(entry)
	if err != nil {
		return nil, fmt.Errorf("failed to extract features: %w", err)
	}

	// Create processed entry
	processedEntry := ProcessedLogEntry{
		Timestamp:  entry.Timestamp,
		SourceType: entry.SourceType,
		SourceID:   entry.SourceID,
		Message:    entry.Message,
		Level:      entry.Level,
		Features:   features,
		Labels:     entry.Labels,
	}

	// Add to training buffer
	ad.addToTrainingBuffer(processedEntry)

	// Check whitelist/blacklist patterns first
	if result := ad.checkPatterns(entry); result != nil {
		return result, nil
	}

	// Get predictions from models
	predictions := make(map[string]float64)
	var maxScore float64
	var bestModel string

	ad.modelsMux.RLock()
	for name, model := range ad.models {
		score, err := model.Predict(processedEntry)
		if err != nil {
			ad.logger.WithError(err).WithField("model", name).Warn("Model prediction failed")
			continue
		}
		predictions[name] = score
		if score > maxScore {
			maxScore = score
			bestModel = name
		}
	}
	ad.modelsMux.RUnlock()

	if len(predictions) == 0 {
		return &AnomalyResult{
			IsAnomaly: false,
			Reason:    "no models available",
		}, nil
	}

	// Calculate ensemble score for ml_ensemble
	var finalScore float64
	if ad.config.Algorithm == "ml_ensemble" {
		finalScore = ad.calculateEnsembleScore(predictions)
	} else {
		finalScore = maxScore
	}

	// Update average score
	ad.updateAverageScore(finalScore)

	// Determine if it's an anomaly
	isAnomaly := finalScore > ad.config.AlertThreshold

	result := &AnomalyResult{
		IsAnomaly:    isAnomaly,
		AnomalyScore: finalScore,
		Confidence:   ad.calculateConfidence(predictions),
		Features:     features,
		ModelUsed:    bestModel,
		Severity:     ad.calculateSeverity(finalScore),
		Reason:       ad.generateReason(finalScore, features),
	}

	// Add recommendations
	result.Recommendations = ad.generateRecommendations(result, entry)

	// Find similar entries
	result.SimilarEntries = ad.findSimilarEntries(processedEntry)

	// Update stats
	if isAnomaly {
		ad.stats.AnomaliesDetected++
		ad.stats.LastAnomalyTime = time.Now()

		ad.logger.WithFields(logrus.Fields{
			"anomaly_score": finalScore,
			"source_type":   entry.SourceType,
			"source_id":     entry.SourceID,
			"severity":      result.Severity,
		}).Warn("Anomaly detected")
	}

	return result, nil
}

// extractFeatures extrai features de um log entry
func (ad *AnomalyDetector) extractFeatures(entry *types.LogEntry) (map[string]float64, error) {
	allFeatures := make(map[string]float64)

	// Extract features using configured extractors
	for _, featureName := range ad.config.Features {
		if extractor, exists := ad.extractors[featureName]; exists {
			features, err := extractor.Extract(entry)
			if err != nil {
				ad.logger.WithError(err).WithField("extractor", featureName).Warn("Feature extraction failed")
				continue
			}

			// Merge features
			for k, v := range features {
				allFeatures[fmt.Sprintf("%s_%s", featureName, k)] = v
			}
		}
	}

	return allFeatures, nil
}

// checkPatterns verifica padrões de whitelist/blacklist
func (ad *AnomalyDetector) checkPatterns(entry *types.LogEntry) *AnomalyResult {
	// Check blacklist patterns (always anomaly)
	for _, pattern := range ad.config.BlacklistPatterns {
		if matched, _ := regexp.MatchString(pattern, entry.Message); matched {
			return &AnomalyResult{
				IsAnomaly:    true,
				AnomalyScore: 1.0,
				Confidence:   1.0,
				ModelUsed:    "blacklist",
				Reason:       fmt.Sprintf("matched blacklist pattern: %s", pattern),
				Severity:     "critical",
			}
		}
	}

	// Check whitelist patterns (never anomaly)
	for _, pattern := range ad.config.WhitelistPatterns {
		if matched, _ := regexp.MatchString(pattern, entry.Message); matched {
			return &AnomalyResult{
				IsAnomaly:    false,
				AnomalyScore: 0.0,
				Confidence:   1.0,
				ModelUsed:    "whitelist",
				Reason:       fmt.Sprintf("matched whitelist pattern: %s", pattern),
				Severity:     "none",
			}
		}
	}

	return nil
}

// calculateEnsembleScore calcula o score ensemble
func (ad *AnomalyDetector) calculateEnsembleScore(predictions map[string]float64) float64 {
	if len(predictions) == 0 {
		return 0.0
	}

	// Weighted average (can be configured)
	weights := map[string]float64{
		"isolation":   0.4,
		"statistical": 0.3,
		"neural":      0.3,
	}

	var totalScore, totalWeight float64
	for model, score := range predictions {
		weight := weights[model]
		if weight == 0 {
			weight = 1.0 / float64(len(predictions)) // Equal weight if not configured
		}
		totalScore += score * weight
		totalWeight += weight
	}

	if totalWeight > 0 {
		return totalScore / totalWeight
	}
	return 0.0
}

// calculateConfidence calcula a confiança da predição
func (ad *AnomalyDetector) calculateConfidence(predictions map[string]float64) float64 {
	if len(predictions) <= 1 {
		return 0.5
	}

	// Calculate standard deviation of predictions
	var mean, variance float64
	for _, score := range predictions {
		mean += score
	}
	mean /= float64(len(predictions))

	for _, score := range predictions {
		variance += math.Pow(score-mean, 2)
	}
	variance /= float64(len(predictions))
	stddev := math.Sqrt(variance)

	// Lower std deviation = higher confidence
	confidence := 1.0 - math.Min(stddev*2, 1.0)
	return confidence
}

// calculateSeverity calcula a severidade da anomalia
func (ad *AnomalyDetector) calculateSeverity(score float64) string {
	switch {
	case score >= 0.9:
		return "critical"
	case score >= 0.8:
		return "high"
	case score >= 0.6:
		return "medium"
	case score >= 0.4:
		return "low"
	default:
		return "none"
	}
}

// generateReason gera uma explicação para o resultado
func (ad *AnomalyDetector) generateReason(score float64, features map[string]float64) string {
	if score < ad.config.AlertThreshold {
		return "normal behavior detected"
	}

	// Find the most anomalous features
	type featureScore struct {
		name  string
		value float64
	}

	var scores []featureScore
	for name, value := range features {
		scores = append(scores, featureScore{name, value})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].value > scores[j].value
	})

	if len(scores) > 0 {
		return fmt.Sprintf("anomalous patterns detected, primary factor: %s (%.2f)", scores[0].name, scores[0].value)
	}

	return "anomalous behavior detected"
}

// generateRecommendations gera recomendações
func (ad *AnomalyDetector) generateRecommendations(result *AnomalyResult, entry *types.LogEntry) []string {
	var recommendations []string

	if !result.IsAnomaly {
		return recommendations
	}

	switch result.Severity {
	case "critical":
		recommendations = append(recommendations, "Immediate investigation required")
		recommendations = append(recommendations, "Consider isolating affected systems")
		recommendations = append(recommendations, "Review security logs")
	case "high":
		recommendations = append(recommendations, "Investigate within 1 hour")
		recommendations = append(recommendations, "Check system resources")
		recommendations = append(recommendations, "Review recent deployments")
	case "medium":
		recommendations = append(recommendations, "Monitor for pattern continuation")
		recommendations = append(recommendations, "Check application health")
	case "low":
		recommendations = append(recommendations, "Log for trend analysis")
	}

	return recommendations
}

// findSimilarEntries encontra entradas similares
func (ad *AnomalyDetector) findSimilarEntries(entry ProcessedLogEntry) []string {
	// Simplified similarity search
	// In a real implementation, this would use more sophisticated algorithms

	var similar []string
	ad.trainingMux.RLock()
	defer ad.trainingMux.RUnlock()

	for i := len(ad.trainingBuffer) - 1; i >= 0 && len(similar) < 3; i-- {
		candidate := ad.trainingBuffer[i]
		if ad.calculateSimilarity(entry, candidate) > 0.8 {
			// Safely truncate message to avoid slice bounds panic
			msg := candidate.Message
			if len(msg) > 50 {
				msg = msg[:50] + "..."
			}
			similar = append(similar, fmt.Sprintf("%s: %s", candidate.Timestamp.Format("15:04:05"), msg))
		}
	}

	return similar
}

// calculateSimilarity calcula similaridade entre duas entradas
func (ad *AnomalyDetector) calculateSimilarity(a, b ProcessedLogEntry) float64 {
	// Simplified similarity calculation
	// Real implementation would use cosine similarity, etc.

	if a.SourceType != b.SourceType {
		return 0.0
	}

	if a.Level != b.Level {
		return 0.5
	}

	// Feature similarity
	var similarity float64
	var count int
	for k, v1 := range a.Features {
		if v2, exists := b.Features[k]; exists {
			diff := math.Abs(v1 - v2)
			similarity += 1.0 - math.Min(diff, 1.0)
			count++
		}
	}

	if count > 0 {
		return similarity / float64(count)
	}

	return 0.0
}

// addToTrainingBuffer adiciona uma entrada ao buffer de treinamento
func (ad *AnomalyDetector) addToTrainingBuffer(entry ProcessedLogEntry) {
	ad.trainingMux.Lock()
	defer ad.trainingMux.Unlock()

	ad.trainingBuffer = append(ad.trainingBuffer, entry)

	// C10: Memory Leak Fix - Reallocate instead of reslice
	// Limit buffer size
	if len(ad.trainingBuffer) > ad.config.MaxTrainingSamples {
		// ❌ OLD (leaks memory): ad.trainingBuffer = ad.trainingBuffer[removeCount:]
		// This keeps the old underlying array in memory!

		// ✅ NEW: Create new slice and copy data
		// This allows old array to be garbage collected
		newBuffer := make([]ProcessedLogEntry, ad.config.MaxTrainingSamples)
		copy(newBuffer, ad.trainingBuffer[len(ad.trainingBuffer)-ad.config.MaxTrainingSamples:])
		ad.trainingBuffer = newBuffer
	}

	ad.stats.TrainingSamples = len(ad.trainingBuffer)
}

// periodicTraining executa treinamento periódico dos modelos
func (ad *AnomalyDetector) periodicTraining() {
	defer ad.wg.Done()

	interval, err := time.ParseDuration(ad.config.TrainingInterval)
	if err != nil {
		ad.logger.WithError(err).Error("Invalid training interval")
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ad.ctx.Done():
			return
		case <-ticker.C:
			if err := ad.trainModels(); err != nil {
				ad.logger.WithError(err).Error("Model training failed")
			}
		}
	}
}

// trainModels treina os modelos com dados do buffer
func (ad *AnomalyDetector) trainModels() error {
	ad.trainingMux.RLock()
	bufferSize := len(ad.trainingBuffer)
	if bufferSize < ad.config.MinTrainingSamples {
		ad.trainingMux.RUnlock()
		ad.logger.WithField("buffer_size", bufferSize).Debug("Not enough training data")
		return nil
	}

	// Copy training data
	trainingData := make([]ProcessedLogEntry, len(ad.trainingBuffer))
	copy(trainingData, ad.trainingBuffer)
	ad.trainingMux.RUnlock()

	ad.logger.WithField("training_samples", len(trainingData)).Info("Starting model training")

	// Train all models
	ad.modelsMux.Lock()
	for name, model := range ad.models {
		if err := model.Train(trainingData); err != nil {
			ad.logger.WithError(err).WithField("model", name).Error("Model training failed")
			continue
		}

		ad.logger.WithFields(logrus.Fields{
			"model":    name,
			"accuracy": model.GetAccuracy(),
		}).Info("Model training completed")
	}
	ad.modelsMux.Unlock()

	ad.stats.LastTrainingTime = time.Now()
	ad.lastTrainingTime = time.Now()

	// Auto-save models after training if configured
	if ad.config.SaveModel && ad.config.ModelPath != "" {
		if err := ad.saveModels(); err != nil {
			ad.logger.WithError(err).Warn("Failed to auto-save models after training")
		} else {
			ad.logger.Info("Models auto-saved successfully after training")
		}
	}

	return nil
}

// updateAverageScore atualiza o score médio
func (ad *AnomalyDetector) updateAverageScore(score float64) {
	// Simple moving average
	alpha := 0.1
	ad.stats.AverageScore = alpha*score + (1-alpha)*ad.stats.AverageScore
}

// saveModels salva os modelos treinados
func (ad *AnomalyDetector) saveModels() error {
	ad.modelsMux.RLock()
	defer ad.modelsMux.RUnlock()

	for name, model := range ad.models {
		modelPath := fmt.Sprintf("%s/%s_model.json", ad.config.ModelPath, name)
		if err := model.Save(modelPath); err != nil {
			ad.logger.WithError(err).WithField("model", name).Error("Failed to save model")
		} else {
			ad.logger.WithField("model", name).Info("Model saved")
		}
	}

	return nil
}

// loadModels carrega modelos salvos
func (ad *AnomalyDetector) loadModels() error {
	ad.modelsMux.Lock()
	defer ad.modelsMux.Unlock()

	for name, model := range ad.models {
		modelPath := fmt.Sprintf("%s/%s_model.json", ad.config.ModelPath, name)
		if err := model.Load(modelPath); err != nil {
			// Log como Debug ao invés de Warn - é esperado que modelos não existam na primeira execução
			ad.logger.WithError(err).WithField("model", name).Debug("Model not found, will be trained from scratch")
		} else {
			ad.logger.WithField("model", name).Info("Model loaded successfully")
		}
	}

	return nil
}

// GetStats retorna as estatísticas atuais
func (ad *AnomalyDetector) GetStats() Stats {
	return ad.stats
}

// IsHealthy verifica se o detector está saudável
func (ad *AnomalyDetector) IsHealthy() bool {
	if !ad.config.Enabled {
		return true
	}

	// Check if we have trained models
	ad.modelsMux.RLock()
	hasModels := len(ad.models) > 0
	ad.modelsMux.RUnlock()

	return hasModels
}

// Export retorna dados para análise externa
func (ad *AnomalyDetector) Export() ([]byte, error) {
	data := map[string]interface{}{
		"stats":           ad.GetStats(),
		"config":          ad.config,
		"training_buffer": len(ad.trainingBuffer),
		"models":          len(ad.models),
	}

	return json.Marshal(data)
}