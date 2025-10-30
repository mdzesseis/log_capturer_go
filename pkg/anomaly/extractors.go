package anomaly

import (
	"crypto/md5"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"ssw-logs-capture/pkg/types"
)

// TextFeatureExtractor extracts text-based features from log entries
type TextFeatureExtractor struct {
	commonPatterns []*regexp.Regexp
	vocabulary     map[string]int
	maxVocabSize   int
}

func NewTextFeatureExtractor() *TextFeatureExtractor {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`), // IP addresses
		regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`), // Email
		regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`), // UUID
		regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`), // Timestamp
		regexp.MustCompile(`\b(ERROR|WARN|INFO|DEBUG|FATAL|PANIC)\b`), // Log levels
		regexp.MustCompile(`\b\d+\b`), // Numbers
		regexp.MustCompile(`\b[A-Z]{2,}\b`), // Uppercase words
	}

	return &TextFeatureExtractor{
		commonPatterns: patterns,
		vocabulary:     make(map[string]int),
		maxVocabSize:   10000,
	}
}

// FeatureExtractor interface implementation

func (te *TextFeatureExtractor) Extract(entry *types.LogEntry) (map[string]float64, error) {
	features := make(map[string]float64)

	message := entry.Message

	// Basic text statistics
	features["message_length"] = float64(len(message))
	features["word_count"] = float64(len(strings.Fields(message)))
	features["char_count"] = float64(len([]rune(message)))

	// Character type ratios
	var digits, letters, spaces, special int
	for _, r := range message {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsLetter(r):
			letters++
		case unicode.IsSpace(r):
			spaces++
		default:
			special++
		}
	}

	total := float64(len([]rune(message)))
	if total > 0 {
		features["digit_ratio"] = float64(digits) / total
		features["letter_ratio"] = float64(letters) / total
		features["space_ratio"] = float64(spaces) / total
		features["special_ratio"] = float64(special) / total
	}

	// Pattern matching
	for i, pattern := range te.commonPatterns {
		matches := pattern.FindAllString(message, -1)
		features[fmt.Sprintf("pattern_%d_count", i)] = float64(len(matches))
	}

	// Entropy calculation
	features["entropy"] = te.calculateEntropy(message)

	// Text complexity
	features["unique_words"] = float64(te.countUniqueWords(message))
	features["avg_word_length"] = te.averageWordLength(message)

	// Case variations
	features["uppercase_count"] = float64(te.countCase(message, true))
	features["lowercase_count"] = float64(te.countCase(message, false))

	// Repetition patterns
	features["repetition_score"] = te.calculateRepetitionScore(message)

	return features, nil
}

func (te *TextFeatureExtractor) GetFeatureNames() []string {
	return []string{
		"message_length",
		"word_count",
		"char_count",
		"digit_ratio",
		"letter_ratio",
		"space_ratio",
		"special_ratio",
		"pattern_0_count",
		"pattern_1_count",
		"pattern_2_count",
		"pattern_3_count",
		"pattern_4_count",
		"pattern_5_count",
		"pattern_6_count",
		"entropy",
		"unique_words",
		"avg_word_length",
		"uppercase_count",
		"lowercase_count",
		"repetition_score",
	}
}

func (te *TextFeatureExtractor) calculateEntropy(text string) float64 {
	charFreq := make(map[rune]int)
	for _, r := range text {
		charFreq[r]++
	}

	length := float64(len([]rune(text)))
	if length == 0 {
		return 0
	}

	entropy := 0.0
	for _, freq := range charFreq {
		p := float64(freq) / length
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}

	return entropy
}

func (te *TextFeatureExtractor) countUniqueWords(text string) int {
	words := strings.Fields(strings.ToLower(text))
	unique := make(map[string]bool)
	for _, word := range words {
		unique[word] = true
	}
	return len(unique)
}

func (te *TextFeatureExtractor) averageWordLength(text string) float64 {
	words := strings.Fields(text)
	if len(words) == 0 {
		return 0
	}

	totalLength := 0
	for _, word := range words {
		totalLength += len(word)
	}

	return float64(totalLength) / float64(len(words))
}

func (te *TextFeatureExtractor) countCase(text string, uppercase bool) int {
	count := 0
	for _, r := range text {
		if uppercase && unicode.IsUpper(r) {
			count++
		} else if !uppercase && unicode.IsLower(r) {
			count++
		}
	}
	return count
}

func (te *TextFeatureExtractor) calculateRepetitionScore(text string) float64 {
	if len(text) < 2 {
		return 0
	}

	repetitions := 0
	for i := 1; i < len(text); i++ {
		if text[i] == text[i-1] {
			repetitions++
		}
	}

	return float64(repetitions) / float64(len(text)-1)
}

// StatisticalFeatureExtractor extracts statistical features from log entries
type StatisticalFeatureExtractor struct {
	window []time.Time
	counts map[string]int
	mu     sync.RWMutex // Protect concurrent access to window and counts
}

func NewStatisticalFeatureExtractor() *StatisticalFeatureExtractor {
	return &StatisticalFeatureExtractor{
		window: make([]time.Time, 0),
		counts: make(map[string]int),
	}
}

// FeatureExtractor interface implementation

func (se *StatisticalFeatureExtractor) Extract(entry *types.LogEntry) (map[string]float64, error) {
	features := make(map[string]float64)

	now := time.Now()

	// Time-based features
	if !entry.Timestamp.IsZero() {
		timeDiff := now.Sub(entry.Timestamp)
		features["time_diff_seconds"] = timeDiff.Seconds()
		features["hour_of_day"] = float64(entry.Timestamp.Hour())
		features["day_of_week"] = float64(entry.Timestamp.Weekday())
		features["day_of_month"] = float64(entry.Timestamp.Day())
		features["month"] = float64(entry.Timestamp.Month())
	}

	// Lock for concurrent access to window and counts
	se.mu.Lock()
	defer se.mu.Unlock()

	// Update sliding window for frequency analysis
	se.updateWindow(now)

	// Frequency features
	features["events_per_minute"] = se.calculateEventsPerMinute()
	features["events_per_hour"] = se.calculateEventsPerHour()

	// Source-based features
	sourceKey := fmt.Sprintf("%s:%s", entry.SourceType, entry.SourceID)
	se.counts[sourceKey]++
	features["source_frequency"] = float64(se.counts[sourceKey])

	// Level-based features
	levelKey := fmt.Sprintf("level:%s", entry.Level)
	se.counts[levelKey]++
	features["level_frequency"] = float64(se.counts[levelKey])

	// Message hash for duplicate detection
	hash := fmt.Sprintf("%x", md5.Sum([]byte(entry.Message)))
	hashKey := fmt.Sprintf("hash:%s", hash)
	se.counts[hashKey]++
	features["message_duplicate_count"] = float64(se.counts[hashKey])

	return features, nil
}

func (se *StatisticalFeatureExtractor) GetFeatureNames() []string {
	return []string{
		"time_diff_seconds",
		"hour_of_day",
		"day_of_week",
		"day_of_month",
		"month",
		"events_per_minute",
		"events_per_hour",
		"source_frequency",
		"level_frequency",
		"message_duplicate_count",
	}
}

func (se *StatisticalFeatureExtractor) updateWindow(now time.Time) {
	// Add current timestamp
	se.window = append(se.window, now)

	// Remove timestamps older than 1 hour
	cutoff := now.Add(-time.Hour)
	validIndex := 0
	for i, t := range se.window {
		if t.After(cutoff) {
			validIndex = i
			break
		}
	}
	se.window = se.window[validIndex:]
}

func (se *StatisticalFeatureExtractor) calculateEventsPerMinute() float64 {
	if len(se.window) == 0 {
		return 0
	}

	now := time.Now()
	cutoff := now.Add(-time.Minute)
	count := 0
	for _, t := range se.window {
		if t.After(cutoff) {
			count++
		}
	}

	return float64(count)
}

func (se *StatisticalFeatureExtractor) calculateEventsPerHour() float64 {
	return float64(len(se.window))
}

// TemporalFeatureExtractor extracts time-based patterns and features
type TemporalFeatureExtractor struct {
	timestamps []time.Time
	intervals  []float64
	mu         sync.Mutex // Protect concurrent access to timestamps and intervals
}

func NewTemporalFeatureExtractor() *TemporalFeatureExtractor {
	return &TemporalFeatureExtractor{
		timestamps: make([]time.Time, 0),
		intervals:  make([]float64, 0),
	}
}

// FeatureExtractor interface implementation

func (te *TemporalFeatureExtractor) Extract(entry *types.LogEntry) (map[string]float64, error) {
	features := make(map[string]float64)

	if entry.Timestamp.IsZero() {
		return features, nil
	}

	// Lock for concurrent access to timestamps and intervals
	te.mu.Lock()
	defer te.mu.Unlock()

	// Add current timestamp
	te.timestamps = append(te.timestamps, entry.Timestamp)

	// Calculate interval features if we have previous timestamps
	if len(te.timestamps) > 1 {
		lastTimestamp := te.timestamps[len(te.timestamps)-2]
		interval := entry.Timestamp.Sub(lastTimestamp).Seconds()
		te.intervals = append(te.intervals, interval)

		// Current interval
		features["current_interval"] = interval

		// Statistical measures of intervals
		if len(te.intervals) > 0 {
			features["avg_interval"] = te.calculateAverage(te.intervals)
			features["std_interval"] = te.calculateStandardDeviation(te.intervals)
			features["min_interval"] = te.calculateMin(te.intervals)
			features["max_interval"] = te.calculateMax(te.intervals)
			features["median_interval"] = te.calculateMedian(te.intervals)
		}

		// Regularity score (how regular are the intervals)
		features["regularity_score"] = te.calculateRegularityScore()
	}

	// Time-of-day features
	features["time_sine"] = math.Sin(2 * math.Pi * float64(entry.Timestamp.Hour()) / 24)
	features["time_cosine"] = math.Cos(2 * math.Pi * float64(entry.Timestamp.Hour()) / 24)

	// Day-of-week features
	features["dow_sine"] = math.Sin(2 * math.Pi * float64(entry.Timestamp.Weekday()) / 7)
	features["dow_cosine"] = math.Cos(2 * math.Pi * float64(entry.Timestamp.Weekday()) / 7)

	// Keep only recent timestamps (last 1000)
	if len(te.timestamps) > 1000 {
		te.timestamps = te.timestamps[len(te.timestamps)-1000:]
	}
	if len(te.intervals) > 1000 {
		te.intervals = te.intervals[len(te.intervals)-1000:]
	}

	return features, nil
}

func (te *TemporalFeatureExtractor) GetFeatureNames() []string {
	return []string{
		"current_interval",
		"avg_interval",
		"std_interval",
		"min_interval",
		"max_interval",
		"median_interval",
		"regularity_score",
		"time_sine",
		"time_cosine",
		"dow_sine",
		"dow_cosine",
	}
}

func (te *TemporalFeatureExtractor) calculateAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (te *TemporalFeatureExtractor) calculateStandardDeviation(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}

	avg := te.calculateAverage(values)
	sumSquares := 0.0
	for _, v := range values {
		diff := v - avg
		sumSquares += diff * diff
	}

	return math.Sqrt(sumSquares / float64(len(values)-1))
}

func (te *TemporalFeatureExtractor) calculateMin(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

func (te *TemporalFeatureExtractor) calculateMax(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}

func (te *TemporalFeatureExtractor) calculateMedian(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

func (te *TemporalFeatureExtractor) calculateRegularityScore() float64 {
	if len(te.intervals) < 3 {
		return 1.0 // Assume regular if not enough data
	}

	// Calculate coefficient of variation (CV = std/mean)
	avg := te.calculateAverage(te.intervals)
	if avg == 0 {
		return 0
	}

	std := te.calculateStandardDeviation(te.intervals)
	cv := std / avg

	// Convert CV to regularity score (lower CV = higher regularity)
	// Use exponential decay to map CV to [0,1] range
	return math.Exp(-cv)
}

// PatternFeatureExtractor extracts pattern-based features using regex and rules
type PatternFeatureExtractor struct {
	errorPatterns   []*regexp.Regexp
	warningPatterns []*regexp.Regexp
	securityPatterns []*regexp.Regexp
	performancePatterns []*regexp.Regexp
}

func NewPatternFeatureExtractor() *PatternFeatureExtractor {
	errorPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(error|exception|fail|crash|abort)`),
		regexp.MustCompile(`(?i)(timeout|deadline exceeded)`),
		regexp.MustCompile(`(?i)(connection refused|connection reset)`),
		regexp.MustCompile(`(?i)(out of memory|memory leak)`),
		regexp.MustCompile(`(?i)(stack overflow|segmentation fault)`),
	}

	warningPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(warn|warning|caution)`),
		regexp.MustCompile(`(?i)(deprecated|obsolete)`),
		regexp.MustCompile(`(?i)(retry|retrying)`),
		regexp.MustCompile(`(?i)(slow|performance)`),
	}

	securityPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(auth|authentication|login|logout)`),
		regexp.MustCompile(`(?i)(access denied|unauthorized|forbidden)`),
		regexp.MustCompile(`(?i)(security|vulnerability|attack)`),
		regexp.MustCompile(`(?i)(malware|virus|trojan)`),
		regexp.MustCompile(`(?i)(injection|xss|csrf)`),
	}

	performancePatterns := []*regexp.Regexp{
		regexp.MustCompile(`\d+ms|\d+\.\d+s|\d+seconds?`), // Response times
		regexp.MustCompile(`\d+%`), // Percentages (CPU, memory, etc.)
		regexp.MustCompile(`\d+MB|\d+GB|\d+KB`), // Memory/disk usage
		regexp.MustCompile(`(?i)(latency|throughput|qps|tps)`),
	}

	return &PatternFeatureExtractor{
		errorPatterns:      errorPatterns,
		warningPatterns:    warningPatterns,
		securityPatterns:   securityPatterns,
		performancePatterns: performancePatterns,
	}
}

// FeatureExtractor interface implementation

func (pe *PatternFeatureExtractor) Extract(entry *types.LogEntry) (map[string]float64, error) {
	features := make(map[string]float64)

	message := strings.ToLower(entry.Message)

	// Error pattern matching
	errorScore := 0.0
	for _, pattern := range pe.errorPatterns {
		if pattern.MatchString(message) {
			errorScore += 1.0
		}
	}
	features["error_pattern_score"] = errorScore

	// Warning pattern matching
	warningScore := 0.0
	for _, pattern := range pe.warningPatterns {
		if pattern.MatchString(message) {
			warningScore += 1.0
		}
	}
	features["warning_pattern_score"] = warningScore

	// Security pattern matching
	securityScore := 0.0
	for _, pattern := range pe.securityPatterns {
		if pattern.MatchString(message) {
			securityScore += 1.0
		}
	}
	features["security_pattern_score"] = securityScore

	// Performance pattern matching
	performanceScore := 0.0
	for _, pattern := range pe.performancePatterns {
		matches := pattern.FindAllString(message, -1)
		performanceScore += float64(len(matches))
	}
	features["performance_pattern_score"] = performanceScore

	// Extract numeric values for performance analysis
	numbers := regexp.MustCompile(`\d+(?:\.\d+)?`).FindAllString(message, -1)
	if len(numbers) > 0 {
		features["numeric_value_count"] = float64(len(numbers))

		// Calculate statistics for numeric values
		values := make([]float64, 0)
		for _, numStr := range numbers {
			if val, err := strconv.ParseFloat(numStr, 64); err == nil {
				values = append(values, val)
			}
		}

		if len(values) > 0 {
			features["max_numeric_value"] = pe.findMax(values)
			features["min_numeric_value"] = pe.findMin(values)
			features["avg_numeric_value"] = pe.calculateSum(values) / float64(len(values))
		}
	}

	// Log level severity scoring
	features["level_severity"] = pe.calculateLevelSeverity(entry.Level)

	return features, nil
}

func (pe *PatternFeatureExtractor) GetFeatureNames() []string {
	return []string{
		"error_pattern_score",
		"warning_pattern_score",
		"security_pattern_score",
		"performance_pattern_score",
		"numeric_value_count",
		"max_numeric_value",
		"min_numeric_value",
		"avg_numeric_value",
		"level_severity",
	}
}

func (pe *PatternFeatureExtractor) findMax(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}

func (pe *PatternFeatureExtractor) findMin(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

func (pe *PatternFeatureExtractor) calculateSum(values []float64) float64 {
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum
}

func (pe *PatternFeatureExtractor) calculateLevelSeverity(level string) float64 {
	switch strings.ToUpper(level) {
	case "TRACE":
		return 1.0
	case "DEBUG":
		return 2.0
	case "INFO":
		return 3.0
	case "WARN", "WARNING":
		return 4.0
	case "ERROR":
		return 5.0
	case "FATAL", "PANIC":
		return 6.0
	default:
		return 3.0 // Default to INFO level
	}
}