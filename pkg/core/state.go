// Package core provides protocol-agnostic enforcement core functionality.
//
// This file implements run state tracking:
// - Monotonically increasing sequence counter (starts at 1)
// - Active call tracking with start times
// - Summary counts for run_end event
//
// Per Interface-Pack §1.5, §1.8:
// - seq is monotonically increasing, starting at 1
// - run_end.summary contains aggregate counts
package core

import (
	"sync"
	"sync/atomic"
	"time"
)

// RunState tracks state for a single run.
type RunState struct {
	// Sequence counter for tool calls (starts at 1)
	seq atomic.Int64

	// Active calls being tracked
	calls   map[string]*CallState
	callsMu sync.RWMutex

	// Summary counters
	summary   Summary
	summaryMu sync.Mutex

	// Run start time for duration calculation
	startTime time.Time
}

// CallState tracks state for a single tool call.
type CallState struct {
	CallID    string
	StartTime time.Time
	Seq       int
}

// Summary contains aggregate counts for the run.
type Summary struct {
	CallsTotal     int
	CallsAllowed   int
	CallsBlocked   int
	CallsThrottled int
	ErrorsTotal    int
}

// NewRunState creates a new RunState.
func NewRunState() *RunState {
	return &RunState{
		calls:     make(map[string]*CallState),
		startTime: time.Now(),
	}
}

// NextSeq returns the next sequence number (thread-safe).
// Sequence starts at 1, not 0.
func (rs *RunState) NextSeq() int {
	return int(rs.seq.Add(1))
}

// StartCall begins tracking a new call.
// Returns the call state with assigned sequence number.
func (rs *RunState) StartCall(callID string) *CallState {
	state := &CallState{
		CallID:    callID,
		StartTime: time.Now(),
		Seq:       rs.NextSeq(),
	}

	rs.callsMu.Lock()
	rs.calls[callID] = state
	rs.callsMu.Unlock()

	return state
}

// EndCall completes tracking for a call.
// Returns the latency in milliseconds, or -1 if call not found.
func (rs *RunState) EndCall(callID string) int {
	rs.callsMu.Lock()
	state, exists := rs.calls[callID]
	if exists {
		delete(rs.calls, callID)
	}
	rs.callsMu.Unlock()

	if !exists {
		return -1
	}

	return int(time.Since(state.StartTime).Milliseconds())
}

// GetCall returns the call state for a call ID, or nil if not found.
func (rs *RunState) GetCall(callID string) *CallState {
	rs.callsMu.RLock()
	defer rs.callsMu.RUnlock()
	return rs.calls[callID]
}

// IncrementAllowed increments the allowed calls counter.
func (rs *RunState) IncrementAllowed() {
	rs.summaryMu.Lock()
	rs.summary.CallsTotal++
	rs.summary.CallsAllowed++
	rs.summaryMu.Unlock()
}

// IncrementBlocked increments the blocked calls counter.
func (rs *RunState) IncrementBlocked() {
	rs.summaryMu.Lock()
	rs.summary.CallsTotal++
	rs.summary.CallsBlocked++
	rs.summaryMu.Unlock()
}

// IncrementThrottled increments the throttled calls counter.
func (rs *RunState) IncrementThrottled() {
	rs.summaryMu.Lock()
	rs.summary.CallsTotal++
	rs.summary.CallsThrottled++
	rs.summaryMu.Unlock()
}

// IncrementErrors increments the error counter.
func (rs *RunState) IncrementErrors() {
	rs.summaryMu.Lock()
	rs.summary.ErrorsTotal++
	rs.summaryMu.Unlock()
}

// GetSummary returns a snapshot of the current summary.
func (rs *RunState) GetSummary() Summary {
	rs.summaryMu.Lock()
	defer rs.summaryMu.Unlock()
	return rs.summary
}

// DurationMS returns the run duration in milliseconds.
func (rs *RunState) DurationMS() int {
	return int(time.Since(rs.startTime).Milliseconds())
}

// StartTime returns the run start time.
func (rs *RunState) StartTime() time.Time {
	return rs.startTime
}
