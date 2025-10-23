package anomaly

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"ssw-logs-capture/pkg/types"
)

// IsolationForestModel implements an isolation forest algorithm for anomaly detection
type IsolationForestModel struct {
	trees        []*IsolationTree
	numTrees     int
	maxSamples   int
	maxDepth     int
	featureNames []string
	trained      bool
}

type IsolationTree struct {
	root *IsolationNode
}

type IsolationNode struct {
	featureName string
	threshold   float64
	left        *IsolationNode
	right       *IsolationNode
	isLeaf      bool
	depth       int
}

func NewIsolationForestModel() *IsolationForestModel {
	return &IsolationForestModel{
		numTrees:   100,
		maxSamples: 256,
		maxDepth:   10,
		trained:    false,
	}
}

func (ifm *IsolationForestModel) Train(features []map[string]float64) error {
	if len(features) == 0 {
		return fmt.Errorf("no training data provided")
	}

	// Extract feature names
	ifm.featureNames = make([]string, 0)
	for name := range features[0] {
		ifm.featureNames = append(ifm.featureNames, name)
	}

	// Build isolation trees
	ifm.trees = make([]*IsolationTree, ifm.numTrees)
	for i := 0; i < ifm.numTrees; i++ {
		// Sample data for this tree
		sampleSize := ifm.maxSamples
		if len(features) < sampleSize {
			sampleSize = len(features)
		}

		sample := ifm.sampleData(features, sampleSize)
		ifm.trees[i] = ifm.buildTree(sample, 0)
	}

	ifm.trained = true
	return nil
}

func (ifm *IsolationForestModel) Predict(features map[string]float64) (float64, error) {
	if !ifm.trained {
		return 0, fmt.Errorf("model not trained")
	}

	totalPathLength := 0.0
	for _, tree := range ifm.trees {
		pathLength := ifm.getPathLength(tree.root, features, 0)
		totalPathLength += pathLength
	}

	avgPathLength := totalPathLength / float64(len(ifm.trees))

	// Normalize using expected path length for normal data
	c := ifm.expectedPathLength(ifm.maxSamples)
	anomalyScore := math.Pow(2, -avgPathLength/c)

	return anomalyScore, nil
}

func (ifm *IsolationForestModel) sampleData(features []map[string]float64, sampleSize int) []map[string]float64 {
	if sampleSize >= len(features) {
		return features
	}

	sample := make([]map[string]float64, sampleSize)
	indices := rand.Perm(len(features))[:sampleSize]

	for i, idx := range indices {
		sample[i] = features[idx]
	}

	return sample
}

func (ifm *IsolationForestModel) buildTree(data []map[string]float64, depth int) *IsolationTree {
	root := ifm.buildNode(data, depth)
	return &IsolationTree{root: root}
}

func (ifm *IsolationForestModel) buildNode(data []map[string]float64, depth int) *IsolationNode {
	node := &IsolationNode{depth: depth}

	// Check termination conditions
	if len(data) <= 1 || depth >= ifm.maxDepth {
		node.isLeaf = true
		return node
	}

	// Check if all samples have same values
	if ifm.allSameFeatures(data) {
		node.isLeaf = true
		return node
	}

	// Randomly select feature and threshold
	featureName := ifm.featureNames[rand.Intn(len(ifm.featureNames))]
	minVal, maxVal := ifm.getFeatureRange(data, featureName)

	if minVal == maxVal {
		node.isLeaf = true
		return node
	}

	threshold := minVal + rand.Float64()*(maxVal-minVal)

	node.featureName = featureName
	node.threshold = threshold

	// Split data
	leftData, rightData := ifm.splitData(data, featureName, threshold)

	// Recursively build children
	if len(leftData) > 0 {
		node.left = ifm.buildNode(leftData, depth+1)
	}
	if len(rightData) > 0 {
		node.right = ifm.buildNode(rightData, depth+1)
	}

	return node
}

func (ifm *IsolationForestModel) allSameFeatures(data []map[string]float64) bool {
	if len(data) <= 1 {
		return true
	}

	first := data[0]
	for i := 1; i < len(data); i++ {
		for name, val := range first {
			if data[i][name] != val {
				return false
			}
		}
	}
	return true
}

func (ifm *IsolationForestModel) getFeatureRange(data []map[string]float64, featureName string) (float64, float64) {
	if len(data) == 0 {
		return 0, 0
	}

	min := data[0][featureName]
	max := min

	for _, sample := range data[1:] {
		val := sample[featureName]
		if val < min {
			min = val
		}
		if val > max {
			max = val
		}
	}

	return min, max
}

func (ifm *IsolationForestModel) splitData(data []map[string]float64, featureName string, threshold float64) ([]map[string]float64, []map[string]float64) {
	var left, right []map[string]float64

	for _, sample := range data {
		if sample[featureName] < threshold {
			left = append(left, sample)
		} else {
			right = append(right, sample)
		}
	}

	return left, right
}

func (ifm *IsolationForestModel) getPathLength(node *IsolationNode, features map[string]float64, currentDepth int) float64 {
	if node == nil || node.isLeaf {
		return float64(currentDepth)
	}

	featureValue := features[node.featureName]
	if featureValue < node.threshold {
		if node.left != nil {
			return ifm.getPathLength(node.left, features, currentDepth+1)
		}
	} else {
		if node.right != nil {
			return ifm.getPathLength(node.right, features, currentDepth+1)
		}
	}

	return float64(currentDepth)
}

func (ifm *IsolationForestModel) expectedPathLength(n int) float64 {
	if n <= 1 {
		return 0
	}
	return 2.0 * (math.Log(float64(n-1)) + 0.5772156649) - (2.0 * float64(n-1) / float64(n))
}

func (ifm *IsolationForestModel) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{
		"type":         "isolation_forest",
		"num_trees":    ifm.numTrees,
		"max_samples":  ifm.maxSamples,
		"max_depth":    ifm.maxDepth,
		"trained":      ifm.trained,
		"num_features": len(ifm.featureNames),
	}
}

// StatisticalModel implements statistical anomaly detection using z-score and percentiles
type StatisticalModel struct {
	means       map[string]float64
	stdDevs     map[string]float64
	percentiles map[string]map[int]float64 // feature -> percentile -> value
	trained     bool
}

func NewStatisticalModel() *StatisticalModel {
	return &StatisticalModel{
		means:       make(map[string]float64),
		stdDevs:     make(map[string]float64),
		percentiles: make(map[string]map[int]float64),
		trained:     false,
	}
}

func (sm *StatisticalModel) Train(features []map[string]float64) error {
	if len(features) == 0 {
		return fmt.Errorf("no training data provided")
	}

	// Calculate means and standard deviations
	featureValues := make(map[string][]float64)

	// Collect all values for each feature
	for _, sample := range features {
		for name, value := range sample {
			if _, exists := featureValues[name]; !exists {
				featureValues[name] = make([]float64, 0)
			}
			featureValues[name] = append(featureValues[name], value)
		}
	}

	// Calculate statistics for each feature
	for name, values := range featureValues {
		sm.means[name] = sm.calculateMean(values)
		sm.stdDevs[name] = sm.calculateStdDev(values, sm.means[name])

		// Calculate percentiles
		sm.percentiles[name] = sm.calculatePercentiles(values)
	}

	sm.trained = true
	return nil
}

func (sm *StatisticalModel) Predict(features map[string]float64) (float64, error) {
	if !sm.trained {
		return 0, fmt.Errorf("model not trained")
	}

	maxAnomalyScore := 0.0

	for name, value := range features {
		if mean, exists := sm.means[name]; exists {
			stdDev := sm.stdDevs[name]

			// Calculate z-score based anomaly
			var zScore float64
			if stdDev > 0 {
				zScore = math.Abs((value - mean) / stdDev)
			}

			// Calculate percentile-based anomaly
			percentileScore := sm.getPercentileAnomalyScore(name, value)

			// Combine z-score and percentile score
			featureAnomalyScore := math.Max(
				sm.zScoreToAnomalyScore(zScore),
				percentileScore,
			)

			if featureAnomalyScore > maxAnomalyScore {
				maxAnomalyScore = featureAnomalyScore
			}
		}
	}

	return maxAnomalyScore, nil
}

func (sm *StatisticalModel) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (sm *StatisticalModel) calculateStdDev(values []float64, mean float64) float64 {
	if len(values) <= 1 {
		return 0
	}

	sumSquares := 0.0
	for _, v := range values {
		diff := v - mean
		sumSquares += diff * diff
	}

	return math.Sqrt(sumSquares / float64(len(values)-1))
}

func (sm *StatisticalModel) calculatePercentiles(values []float64) map[int]float64 {
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	percentiles := map[int]float64{
		1:  sm.getPercentile(sorted, 0.01),
		5:  sm.getPercentile(sorted, 0.05),
		25: sm.getPercentile(sorted, 0.25),
		50: sm.getPercentile(sorted, 0.50),
		75: sm.getPercentile(sorted, 0.75),
		95: sm.getPercentile(sorted, 0.95),
		99: sm.getPercentile(sorted, 0.99),
	}

	return percentiles
}

func (sm *StatisticalModel) getPercentile(sortedValues []float64, p float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}

	index := p * float64(len(sortedValues)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))

	if lower == upper {
		return sortedValues[lower]
	}

	weight := index - math.Floor(index)
	return sortedValues[lower]*(1-weight) + sortedValues[upper]*weight
}

func (sm *StatisticalModel) getPercentileAnomalyScore(featureName string, value float64) float64 {
	percentiles, exists := sm.percentiles[featureName]
	if !exists {
		return 0
	}

	// Check if value is outside extreme percentiles
	if value < percentiles[1] || value > percentiles[99] {
		return 0.9 // High anomaly score for extreme outliers
	}
	if value < percentiles[5] || value > percentiles[95] {
		return 0.7 // Medium-high anomaly score
	}
	if value < percentiles[25] || value > percentiles[75] {
		return 0.3 // Low-medium anomaly score
	}

	return 0.1 // Normal range
}

func (sm *StatisticalModel) zScoreToAnomalyScore(zScore float64) float64 {
	// Convert z-score to anomaly score using sigmoid function
	// Higher z-scores get higher anomaly scores
	if zScore > 3.0 {
		return 0.95 // Very high anomaly
	}
	if zScore > 2.0 {
		return 0.8 // High anomaly
	}
	if zScore > 1.5 {
		return 0.6 // Medium anomaly
	}
	if zScore > 1.0 {
		return 0.4 // Low-medium anomaly
	}

	return 0.1 // Normal
}

func (sm *StatisticalModel) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{
		"type":         "statistical",
		"trained":      sm.trained,
		"num_features": len(sm.means),
		"features":     sm.getFeatureStats(),
	}
}

func (sm *StatisticalModel) getFeatureStats() map[string]map[string]float64 {
	stats := make(map[string]map[string]float64)

	for name, mean := range sm.means {
		stats[name] = map[string]float64{
			"mean":   mean,
			"stddev": sm.stdDevs[name],
		}
	}

	return stats
}

// NeuralNetworkModel implements a simple neural network for anomaly detection
type NeuralNetworkModel struct {
	inputSize    int
	hiddenSize   int
	outputSize   int
	weightsIH    [][]float64 // input to hidden weights
	weightsHO    [][]float64 // hidden to output weights
	biasH        []float64   // hidden layer bias
	biasO        []float64   // output layer bias
	learningRate float64
	trained      bool
	featureNames []string
}

func NewNeuralNetworkModel() *NeuralNetworkModel {
	return &NeuralNetworkModel{
		hiddenSize:   20,
		outputSize:   1,
		learningRate: 0.01,
		trained:      false,
	}
}

func (nnm *NeuralNetworkModel) Train(features []map[string]float64) error {
	if len(features) == 0 {
		return fmt.Errorf("no training data provided")
	}

	// Initialize network dimensions
	nnm.featureNames = make([]string, 0)
	for name := range features[0] {
		nnm.featureNames = append(nnm.featureNames, name)
	}
	nnm.inputSize = len(nnm.featureNames)

	// Initialize weights and biases
	nnm.initializeWeights()

	// Convert training data to matrix format
	inputs := nnm.featuresToMatrix(features)

	// For unsupervised anomaly detection, we train an autoencoder
	// Target output is the same as input (reconstruction)
	targets := inputs

	// Training loop
	epochs := 100
	for epoch := 0; epoch < epochs; epoch++ {
		totalLoss := 0.0

		for i := 0; i < len(inputs); i++ {
			// Forward pass
			hidden := nnm.forwardHidden(inputs[i])
			output := nnm.forwardOutput(hidden)

			// Calculate loss (mean squared error)
			loss := nnm.calculateLoss(output, targets[i])
			totalLoss += loss

			// Backward pass
			nnm.backpropagate(inputs[i], hidden, output, targets[i])
		}

		// Optional: print training progress
		if epoch%20 == 0 {
			avgLoss := totalLoss / float64(len(inputs))
			_ = avgLoss // Suppress unused variable warning
		}
	}

	nnm.trained = true
	return nil
}

func (nnm *NeuralNetworkModel) Predict(features map[string]float64) (float64, error) {
	if !nnm.trained {
		return 0, fmt.Errorf("model not trained")
	}

	// Convert features to input vector
	input := nnm.featuresToVector(features)

	// Forward pass
	hidden := nnm.forwardHidden(input)
	output := nnm.forwardOutput(hidden)

	// Calculate reconstruction error as anomaly score
	reconstructionError := nnm.calculateLoss(output, input)

	// Normalize to [0,1] range using sigmoid
	anomalyScore := 1.0 / (1.0 + math.Exp(-reconstructionError))

	return anomalyScore, nil
}

func (nnm *NeuralNetworkModel) initializeWeights() {
	// Initialize input to hidden weights
	nnm.weightsIH = make([][]float64, nnm.inputSize)
	for i := range nnm.weightsIH {
		nnm.weightsIH[i] = make([]float64, nnm.hiddenSize)
		for j := range nnm.weightsIH[i] {
			nnm.weightsIH[i][j] = (rand.Float64() - 0.5) * 2.0 / math.Sqrt(float64(nnm.inputSize))
		}
	}

	// Initialize hidden to output weights
	nnm.weightsHO = make([][]float64, nnm.hiddenSize)
	for i := range nnm.weightsHO {
		nnm.weightsHO[i] = make([]float64, nnm.outputSize)
		for j := range nnm.weightsHO[i] {
			nnm.weightsHO[i][j] = (rand.Float64() - 0.5) * 2.0 / math.Sqrt(float64(nnm.hiddenSize))
		}
	}

	// Initialize biases
	nnm.biasH = make([]float64, nnm.hiddenSize)
	nnm.biasO = make([]float64, nnm.outputSize)
}

func (nnm *NeuralNetworkModel) featuresToMatrix(features []map[string]float64) [][]float64 {
	matrix := make([][]float64, len(features))
	for i, sample := range features {
		matrix[i] = nnm.featuresToVector(sample)
	}
	return matrix
}

func (nnm *NeuralNetworkModel) featuresToVector(features map[string]float64) []float64 {
	vector := make([]float64, len(nnm.featureNames))
	for i, name := range nnm.featureNames {
		if val, exists := features[name]; exists {
			vector[i] = val
		}
	}
	return vector
}

func (nnm *NeuralNetworkModel) forwardHidden(input []float64) []float64 {
	hidden := make([]float64, nnm.hiddenSize)

	for j := 0; j < nnm.hiddenSize; j++ {
		sum := nnm.biasH[j]
		for i := 0; i < nnm.inputSize; i++ {
			sum += input[i] * nnm.weightsIH[i][j]
		}
		hidden[j] = nnm.sigmoid(sum)
	}

	return hidden
}

func (nnm *NeuralNetworkModel) forwardOutput(hidden []float64) []float64 {
	output := make([]float64, nnm.outputSize)

	for j := 0; j < nnm.outputSize; j++ {
		sum := nnm.biasO[j]
		for i := 0; i < nnm.hiddenSize; i++ {
			sum += hidden[i] * nnm.weightsHO[i][j]
		}
		output[j] = sum // Linear output for autoencoder
	}

	return output
}

func (nnm *NeuralNetworkModel) sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}

func (nnm *NeuralNetworkModel) sigmoidDerivative(x float64) float64 {
	s := nnm.sigmoid(x)
	return s * (1.0 - s)
}

func (nnm *NeuralNetworkModel) calculateLoss(output, target []float64) float64 {
	loss := 0.0
	for i := 0; i < len(output); i++ {
		diff := output[i] - target[i]
		loss += diff * diff
	}
	return loss / float64(len(output))
}

func (nnm *NeuralNetworkModel) backpropagate(input, hidden, output, target []float64) {
	// Calculate output layer errors
	outputErrors := make([]float64, nnm.outputSize)
	for j := 0; j < nnm.outputSize; j++ {
		outputErrors[j] = target[j] - output[j]
	}

	// Calculate hidden layer errors
	hiddenErrors := make([]float64, nnm.hiddenSize)
	for i := 0; i < nnm.hiddenSize; i++ {
		error := 0.0
		for j := 0; j < nnm.outputSize; j++ {
			error += outputErrors[j] * nnm.weightsHO[i][j]
		}
		hiddenErrors[i] = error * nnm.sigmoidDerivative(hidden[i])
	}

	// Update hidden to output weights
	for i := 0; i < nnm.hiddenSize; i++ {
		for j := 0; j < nnm.outputSize; j++ {
			nnm.weightsHO[i][j] += nnm.learningRate * outputErrors[j] * hidden[i]
		}
	}

	// Update output biases
	for j := 0; j < nnm.outputSize; j++ {
		nnm.biasO[j] += nnm.learningRate * outputErrors[j]
	}

	// Update input to hidden weights
	for i := 0; i < nnm.inputSize; i++ {
		for j := 0; j < nnm.hiddenSize; j++ {
			nnm.weightsIH[i][j] += nnm.learningRate * hiddenErrors[j] * input[i]
		}
	}

	// Update hidden biases
	for j := 0; j < nnm.hiddenSize; j++ {
		nnm.biasH[j] += nnm.learningRate * hiddenErrors[j]
	}
}

func (nnm *NeuralNetworkModel) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{
		"type":          "neural_network",
		"input_size":    nnm.inputSize,
		"hidden_size":   nnm.hiddenSize,
		"output_size":   nnm.outputSize,
		"learning_rate": nnm.learningRate,
		"trained":       nnm.trained,
	}
}

// EnsembleModel combines multiple models for improved accuracy
type EnsembleModel struct {
	models       []AnomalyModel
	weights      []float64
	trained      bool
	votingMethod string // "average", "weighted", "majority"
}

func NewEnsembleModel(models []AnomalyModel) *EnsembleModel {
	weights := make([]float64, len(models))
	for i := range weights {
		weights[i] = 1.0 / float64(len(models)) // Equal weights initially
	}

	return &EnsembleModel{
		models:       models,
		weights:      weights,
		trained:      false,
		votingMethod: "weighted",
	}
}

func (em *EnsembleModel) Train(features []map[string]float64) error {
	if len(features) == 0 {
		return fmt.Errorf("no training data provided")
	}

	// Train all models
	for i, model := range em.models {
		if err := model.Train(features); err != nil {
			return fmt.Errorf("failed to train model %d: %v", i, err)
		}
	}

	em.trained = true
	return nil
}

func (em *EnsembleModel) Predict(features map[string]float64) (float64, error) {
	if !em.trained {
		return 0, fmt.Errorf("ensemble model not trained")
	}

	if len(em.models) == 0 {
		return 0, fmt.Errorf("no models in ensemble")
	}

	predictions := make([]float64, len(em.models))
	for i, model := range em.models {
		pred, err := model.Predict(features)
		if err != nil {
			// If one model fails, use 0 as prediction
			predictions[i] = 0
		} else {
			predictions[i] = pred
		}
	}

	switch em.votingMethod {
	case "average":
		return em.averageVoting(predictions), nil
	case "weighted":
		return em.weightedVoting(predictions), nil
	case "majority":
		return em.majorityVoting(predictions), nil
	default:
		return em.weightedVoting(predictions), nil
	}
}

func (em *EnsembleModel) averageVoting(predictions []float64) float64 {
	sum := 0.0
	for _, pred := range predictions {
		sum += pred
	}
	return sum / float64(len(predictions))
}

func (em *EnsembleModel) weightedVoting(predictions []float64) float64 {
	weightedSum := 0.0
	totalWeight := 0.0

	for i, pred := range predictions {
		weight := em.weights[i]
		weightedSum += pred * weight
		totalWeight += weight
	}

	if totalWeight > 0 {
		return weightedSum / totalWeight
	}
	return 0
}

func (em *EnsembleModel) majorityVoting(predictions []float64) float64 {
	// Convert to binary decisions using threshold 0.5
	anomalyCount := 0
	for _, pred := range predictions {
		if pred > 0.5 {
			anomalyCount++
		}
	}

	// Return proportion of models that detected anomaly
	return float64(anomalyCount) / float64(len(predictions))
}

func (em *EnsembleModel) SetWeights(weights []float64) error {
	if len(weights) != len(em.models) {
		return fmt.Errorf("number of weights (%d) must match number of models (%d)", len(weights), len(em.models))
	}

	// Normalize weights
	totalWeight := 0.0
	for _, w := range weights {
		totalWeight += w
	}

	if totalWeight > 0 {
		em.weights = make([]float64, len(weights))
		for i, w := range weights {
			em.weights[i] = w / totalWeight
		}
	}

	return nil
}

func (em *EnsembleModel) GetModelInfo() map[string]interface{} {
	modelInfos := make([]map[string]interface{}, len(em.models))
	for i, model := range em.models {
		modelInfos[i] = model.GetModelInfo()
	}

	return map[string]interface{}{
		"type":          "ensemble",
		"num_models":    len(em.models),
		"voting_method": em.votingMethod,
		"weights":       em.weights,
		"models":        modelInfos,
		"trained":       em.trained,
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}