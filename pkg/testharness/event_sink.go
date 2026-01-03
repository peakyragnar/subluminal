// Package testharness provides test infrastructure for contract testing.
//
// This file implements an event sink that captures JSONL events for testing.
//
// WHY THIS EXISTS:
// The shim emits events as JSONL (one JSON object per line).
// To test that the shim emits correct events, we need to:
//   - Capture all events
//   - Parse them into structured data
//   - Query them for assertions ("was run_id present?", "were events in order?")
package testharness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// =============================================================================
// Event represents a captured event from the shim.
// We use map[string]any because events have varying shapes by type.
// =============================================================================

// CapturedEvent holds a parsed event plus the raw JSON.
type CapturedEvent struct {
	// Raw is the original JSON line (for golden comparisons).
	Raw string

	// Parsed is the event as a map (for field access).
	Parsed map[string]any

	// Type is extracted from parsed["type"] for convenience.
	Type string

	// Index is the order this event was received (0-based).
	Index int
}

// =============================================================================
// EventSink collects and queries events.
// =============================================================================

// EventSink captures JSONL events from a reader.
type EventSink struct {
	mu     sync.Mutex
	events []CapturedEvent
	errors []error // Parse errors encountered
}

// NewEventSink creates a new empty event sink.
func NewEventSink() *EventSink {
	return &EventSink{
		events: []CapturedEvent{},
		errors: []error{},
	}
}

// Capture reads JSONL from r until EOF, storing all events.
// This blocks until the reader is closed.
// Typically run in a goroutine: go sink.Capture(shimStdout)
func (s *EventSink) Capture(r io.Reader) error {
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue // Skip empty lines
		}

		// Parse the JSON
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			s.mu.Lock()
			s.errors = append(s.errors, fmt.Errorf("parse error on line %d: %w", len(s.events), err))
			s.mu.Unlock()
			continue
		}

		// Extract type for convenience
		eventType, _ := parsed["type"].(string)

		// Store the event
		s.mu.Lock()
		s.events = append(s.events, CapturedEvent{
			Raw:    line,
			Parsed: parsed,
			Type:   eventType,
			Index:  len(s.events),
		})
		s.mu.Unlock()
	}

	return scanner.Err()
}

// =============================================================================
// Query methods for test assertions
// =============================================================================

// All returns all captured events.
func (s *EventSink) All() []CapturedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CapturedEvent{}, s.events...) // Return a copy
}

// Count returns the number of captured events.
func (s *EventSink) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// Errors returns any parse errors encountered.
func (s *EventSink) Errors() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error{}, s.errors...)
}

// ByType returns all events of a specific type.
// Example: sink.ByType("tool_call_start")
func (s *EventSink) ByType(eventType string) []CapturedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []CapturedEvent
	for _, e := range s.events {
		if e.Type == eventType {
			result = append(result, e)
		}
	}
	return result
}

// WaitForCount waits until the total event count reaches the target or timeout.
func (s *EventSink) WaitForCount(count int, timeout time.Duration) bool {
	if count <= 0 {
		return true
	}
	if timeout <= 0 {
		return s.Count() >= count
	}

	deadline := time.Now().Add(timeout)
	for {
		if s.Count() >= count {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// WaitForTypeCount waits until the event type count reaches the target or timeout.
func (s *EventSink) WaitForTypeCount(eventType string, count int, timeout time.Duration) bool {
	if count <= 0 {
		return true
	}
	if timeout <= 0 {
		return len(s.ByType(eventType)) >= count
	}

	deadline := time.Now().Add(timeout)
	for {
		if len(s.ByType(eventType)) >= count {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// First returns the first event, or nil if none.
func (s *EventSink) First() *CapturedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) == 0 {
		return nil
	}
	return &s.events[0]
}

// Last returns the last event, or nil if none.
func (s *EventSink) Last() *CapturedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) == 0 {
		return nil
	}
	return &s.events[len(s.events)-1]
}

// FirstOfType returns the first event of a specific type, or nil.
func (s *EventSink) FirstOfType(eventType string) *CapturedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if e.Type == eventType {
			return &e
		}
	}
	return nil
}

// Types returns the event types in order (for sequence assertions).
// Example: sink.Types() == []string{"run_start", "tool_call_start", ...}
func (s *EventSink) Types() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	types := make([]string, len(s.events))
	for i, e := range s.events {
		types[i] = e.Type
	}
	return types
}

// =============================================================================
// Field extraction helpers
// =============================================================================

// GetField extracts a field from an event by dot-path.
// Example: GetField(event, "source.host_id") -> "host_123"
// Returns nil if path doesn't exist.
func GetField(e CapturedEvent, path string) any {
	return getNestedField(e.Parsed, path)
}

// GetString extracts a string field, returns "" if not found or not a string.
func GetString(e CapturedEvent, path string) string {
	v := getNestedField(e.Parsed, path)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// GetInt extracts an int field, returns 0 if not found.
// JSON numbers are float64, so we convert.
func GetInt(e CapturedEvent, path string) int {
	v := getNestedField(e.Parsed, path)
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// GetBool extracts a bool field, returns false if not found.
func GetBool(e CapturedEvent, path string) bool {
	v := getNestedField(e.Parsed, path)
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// HasField checks if a field exists (even if nil).
func HasField(e CapturedEvent, path string) bool {
	return hasNestedField(e.Parsed, path)
}

// =============================================================================
// Internal helpers
// =============================================================================

// getNestedField navigates a dot-separated path.
// "source.host_id" -> m["source"]["host_id"]
func getNestedField(m map[string]any, path string) any {
	keys := splitPath(path)
	current := any(m)

	for _, key := range keys {
		if cm, ok := current.(map[string]any); ok {
			current = cm[key]
		} else {
			return nil
		}
	}
	return current
}

// hasNestedField checks if a path exists in the map.
func hasNestedField(m map[string]any, path string) bool {
	keys := splitPath(path)
	current := any(m)

	for i, key := range keys {
		cm, ok := current.(map[string]any)
		if !ok {
			return false
		}
		val, exists := cm[key]
		if !exists {
			return false
		}
		if i < len(keys)-1 {
			current = val
		}
	}
	return true
}

// splitPath splits "a.b.c" into ["a", "b", "c"].
func splitPath(path string) []string {
	var result []string
	current := ""
	for _, c := range path {
		if c == '.' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// =============================================================================
// Assertion helpers (for cleaner test code)
// =============================================================================

// AssertEventOrder checks that events appear in the expected type order.
// Returns an error describing the mismatch, or nil if correct.
func (s *EventSink) AssertEventOrder(expectedTypes ...string) error {
	actual := s.Types()

	if len(actual) < len(expectedTypes) {
		return fmt.Errorf("expected at least %d events, got %d\nexpected types: %v\nactual types: %v",
			len(expectedTypes), len(actual), expectedTypes, actual)
	}

	// Check that expectedTypes appear in order (not necessarily consecutive)
	actualIdx := 0
	for _, expected := range expectedTypes {
		found := false
		for actualIdx < len(actual) {
			if actual[actualIdx] == expected {
				found = true
				actualIdx++
				break
			}
			actualIdx++
		}
		if !found {
			return fmt.Errorf("expected event type %q not found in order\nexpected types: %v\nactual types: %v",
				expected, expectedTypes, actual)
		}
	}
	return nil
}

// AssertAllHaveField checks that all events have a specific field.
func (s *EventSink) AssertAllHaveField(field string) error {
	for _, e := range s.All() {
		if !HasField(e, field) {
			return fmt.Errorf("event %d (type=%s) missing field %q", e.Index, e.Type, field)
		}
	}
	return nil
}

// AssertAllHaveNonEmptyField checks that all events have a non-empty string field.
func (s *EventSink) AssertAllHaveNonEmptyField(field string) error {
	for _, e := range s.All() {
		val := GetString(e, field)
		if val == "" {
			return fmt.Errorf("event %d (type=%s) has empty or missing field %q", e.Index, e.Type, field)
		}
	}
	return nil
}

// AssertFieldConsistent checks that all events have the same value for a field.
func (s *EventSink) AssertFieldConsistent(field string) error {
	events := s.All()
	if len(events) == 0 {
		return nil
	}

	firstVal := GetField(events[0], field)
	for _, e := range events[1:] {
		val := GetField(e, field)
		if fmt.Sprintf("%v", val) != fmt.Sprintf("%v", firstVal) {
			return fmt.Errorf("field %q inconsistent: event 0 has %v, event %d has %v",
				field, firstVal, e.Index, val)
		}
	}
	return nil
}
