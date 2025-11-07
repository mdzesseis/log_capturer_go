package tracing

import (
	"math/rand"
	"sync"
	"time"
)

// OnDemandRule represents a temporary tracing rule for a specific source
type OnDemandRule struct {
	SourceID   string
	SampleRate float64
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// OnDemandController manages on-demand tracing rules
type OnDemandController struct {
	rules map[string]*OnDemandRule
	mu    sync.RWMutex
}

// NewOnDemandController creates a new on-demand controller
func NewOnDemandController() *OnDemandController {
	odc := &OnDemandController{
		rules: make(map[string]*OnDemandRule),
	}

	// Start cleanup goroutine
	go odc.cleanupExpiredRules()

	return odc
}

// Enable enables on-demand tracing for a specific source
func (odc *OnDemandController) Enable(sourceID string, rate float64, duration time.Duration) {
	odc.mu.Lock()
	defer odc.mu.Unlock()

	// Validate rate
	if rate < 0.0 {
		rate = 0.0
	}
	if rate > 1.0 {
		rate = 1.0
	}

	odc.rules[sourceID] = &OnDemandRule{
		SourceID:   sourceID,
		SampleRate: rate,
		ExpiresAt:  time.Now().Add(duration),
		CreatedAt:  time.Now(),
	}
}

// Disable disables on-demand tracing for a specific source
func (odc *OnDemandController) Disable(sourceID string) {
	odc.mu.Lock()
	defer odc.mu.Unlock()

	delete(odc.rules, sourceID)
}

// ShouldTrace checks if a source should be traced based on on-demand rules
func (odc *OnDemandController) ShouldTrace(sourceID string) bool {
	odc.mu.RLock()
	defer odc.mu.RUnlock()

	rule, exists := odc.rules[sourceID]
	if !exists {
		return false
	}

	// Check expiration
	if time.Now().After(rule.ExpiresAt) {
		return false
	}

	// Check sample rate
	return rand.Float64() < rule.SampleRate
}

// GetActiveRules returns all active on-demand rules
func (odc *OnDemandController) GetActiveRules() []map[string]interface{} {
	odc.mu.RLock()
	defer odc.mu.RUnlock()

	rules := make([]map[string]interface{}, 0, len(odc.rules))

	for _, rule := range odc.rules {
		// Skip expired rules
		if time.Now().After(rule.ExpiresAt) {
			continue
		}

		rules = append(rules, map[string]interface{}{
			"source_id":   rule.SourceID,
			"sample_rate": rule.SampleRate,
			"expires_at":  rule.ExpiresAt.Format(time.RFC3339),
			"created_at":  rule.CreatedAt.Format(time.RFC3339),
			"remaining":   time.Until(rule.ExpiresAt).String(),
		})
	}

	return rules
}

// GetRule retrieves a specific rule
func (odc *OnDemandController) GetRule(sourceID string) (*OnDemandRule, bool) {
	odc.mu.RLock()
	defer odc.mu.RUnlock()

	rule, exists := odc.rules[sourceID]
	if !exists {
		return nil, false
	}

	// Check if expired
	if time.Now().After(rule.ExpiresAt) {
		return nil, false
	}

	return rule, true
}

// Count returns the number of active rules
func (odc *OnDemandController) Count() int {
	odc.mu.RLock()
	defer odc.mu.RUnlock()

	count := 0
	for _, rule := range odc.rules {
		if time.Now().Before(rule.ExpiresAt) {
			count++
		}
	}

	return count
}

// cleanupExpiredRules periodically removes expired rules
func (odc *OnDemandController) cleanupExpiredRules() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		odc.mu.Lock()

		now := time.Now()
		for sourceID, rule := range odc.rules {
			if now.After(rule.ExpiresAt) {
				delete(odc.rules, sourceID)
			}
		}

		odc.mu.Unlock()
	}
}

// Clear removes all rules
func (odc *OnDemandController) Clear() {
	odc.mu.Lock()
	defer odc.mu.Unlock()

	odc.rules = make(map[string]*OnDemandRule)
}
