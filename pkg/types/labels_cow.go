// Package types provides core data structures for log processing.
package types

import (
	"encoding/json"
	"sync"
)

// MarshalJSON implements json.Marshaler for LabelsCOW.
func (l *LabelsCOW) MarshalJSON() ([]byte, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return json.Marshal(l.data)
}

// UnmarshalJSON implements json.Unmarshaler for LabelsCOW.
func (l *LabelsCOW) UnmarshalJSON(data []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.data == nil {
		l.data = make(map[string]string)
	}
	return json.Unmarshal(data, &l.data)
}

// LabelsCOW implements a Copy-on-Write labels structure that is thread-safe
// and allows efficient sharing of label maps between LogEntries.
//
// When marked as readonly, any modification attempt will trigger a deep copy
// first, ensuring the original data remains unchanged. This enables zero-copy
// sharing for read-only access patterns.
type LabelsCOW struct {
	mu       sync.RWMutex
	data     map[string]string
	readonly bool
}

// NewLabelsCOW creates a new LabelsCOW with an empty map.
func NewLabelsCOW() *LabelsCOW {
	return &LabelsCOW{
		data: make(map[string]string),
	}
}

// NewLabelsCOWFromMap creates a new LabelsCOW from an existing map.
// The map is copied to ensure independence from the source.
func NewLabelsCOWFromMap(m map[string]string) *LabelsCOW {
	data := make(map[string]string, len(m))
	for k, v := range m {
		data[k] = v
	}
	return &LabelsCOW{
		data: data,
	}
}

// Get retrieves a value by key. Returns empty string and false if not found.
func (l *LabelsCOW) Get(key string) (string, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	val, ok := l.data[key]
	return val, ok
}

// Set sets a key-value pair. If the LabelsCOW is readonly, it performs
// a copy-on-write before modifying.
func (l *LabelsCOW) Set(key, value string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.copyOnWriteIfNeeded()
	l.data[key] = value
}

// Delete removes a key. If the LabelsCOW is readonly, it performs
// a copy-on-write before modifying.
func (l *LabelsCOW) Delete(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.copyOnWriteIfNeeded()
	delete(l.data, key)
}

// Range iterates over all key-value pairs. The function f is called for each pair.
// If f returns false, iteration stops.
// Note: The function f is called while holding the read lock.
func (l *LabelsCOW) Range(f func(key, value string) bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for k, v := range l.data {
		if !f(k, v) {
			break
		}
	}
}

// Len returns the number of labels.
func (l *LabelsCOW) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.data)
}

// Clone creates a deep copy of the LabelsCOW.
// The clone is not marked as readonly regardless of the source's state.
func (l *LabelsCOW) Clone() *LabelsCOW {
	l.mu.RLock()
	defer l.mu.RUnlock()

	data := make(map[string]string, len(l.data))
	for k, v := range l.data {
		data[k] = v
	}
	return &LabelsCOW{
		data:     data,
		readonly: false,
	}
}

// MarkReadOnly marks this LabelsCOW as readonly. Any subsequent modifications
// will trigger a copy-on-write operation first.
func (l *LabelsCOW) MarkReadOnly() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.readonly = true
}

// IsReadOnly returns whether this LabelsCOW is marked as readonly.
func (l *LabelsCOW) IsReadOnly() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.readonly
}

// ToMap returns a copy of the internal map.
// This is useful for backward compatibility with code expecting map[string]string.
func (l *LabelsCOW) ToMap() map[string]string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make(map[string]string, len(l.data))
	for k, v := range l.data {
		result[k] = v
	}
	return result
}

// Merge adds all key-value pairs from the given map. If the LabelsCOW is readonly,
// it performs a copy-on-write before modifying.
func (l *LabelsCOW) Merge(m map[string]string) {
	if len(m) == 0 {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.copyOnWriteIfNeeded()

	for k, v := range m {
		l.data[k] = v
	}
}

// copyOnWriteIfNeeded performs a deep copy of the internal map if readonly.
// Must be called while holding the write lock.
func (l *LabelsCOW) copyOnWriteIfNeeded() {
	if !l.readonly {
		return
	}

	newData := make(map[string]string, len(l.data))
	for k, v := range l.data {
		newData[k] = v
	}
	l.data = newData
	l.readonly = false
}

// ShallowCopy creates a shallow copy that shares the same underlying data.
// Both the original and the copy are marked as readonly to enable COW semantics.
// This is useful for efficient sharing in BatchProcessor.
func (l *LabelsCOW) ShallowCopy() *LabelsCOW {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Mark source as readonly to trigger COW on modification
	l.readonly = true

	return &LabelsCOW{
		data:     l.data, // Share the same map
		readonly: true,   // Mark copy as readonly too
	}
}

// Clear removes all entries. If the LabelsCOW is readonly, it creates a new
// empty map instead of clearing the shared one.
func (l *LabelsCOW) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.readonly {
		// Create new empty map instead of clearing shared one
		l.data = make(map[string]string)
		l.readonly = false
	} else {
		// Clear in place
		for k := range l.data {
			delete(l.data, k)
		}
	}
}

// Has checks if a key exists.
func (l *LabelsCOW) Has(key string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.data[key]
	return ok
}
