// Package core provides protocol-agnostic enforcement core functionality.
//
// This file implements async JSONL event emission to stderr.
// Events are serialized and written in a background goroutine to avoid
// blocking the main request/response path.
//
// Per Interface-Pack §1.1:
// - JSONL format: one JSON object per line, UTF-8, \n terminator
// - Events MUST NOT be multi-line
package core

import (
	"io"
	"sync"

	"github.com/subluminal/subluminal/pkg/event"
)

const (
	// DefaultBufferSize is the default event queue size.
	// Events are dropped if the queue is full (non-blocking emit).
	DefaultBufferSize = 1000
)

// EmitterOptions configures buffer sizing and backpressure behavior.
type EmitterOptions struct {
	BufferSize           int
	PreviewDropThreshold int
}

type eventKind int

const (
	eventKindNormal eventKind = iota
	eventKindPreview
	eventKindDecision
)

// Emitter handles async event emission to a writer (typically stderr).
type Emitter struct {
	writer io.Writer

	mu                   sync.Mutex
	notEmpty             *sync.Cond
	notFull              *sync.Cond
	queue                []queuedEvent
	capacity             int
	previewDropThreshold int
	closed               bool
	wg                   sync.WaitGroup
}

type queuedEvent struct {
	data []byte
	done chan struct{}
	kind eventKind
}

// NewEmitter creates a new Emitter that writes to the given writer.
func NewEmitter(w io.Writer) *Emitter {
	return NewEmitterWithOptions(w, EmitterOptions{})
}

// NewEmitterWithOptions creates a new Emitter with configurable buffer sizing.
func NewEmitterWithOptions(w io.Writer, opts EmitterOptions) *Emitter {
	bufferSize := opts.BufferSize
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	threshold := opts.PreviewDropThreshold
	if threshold <= 0 || threshold > bufferSize {
		threshold = (bufferSize * 3) / 4
		if threshold < 1 {
			threshold = 1
		}
	}

	e := &Emitter{
		writer:               w,
		queue:                make([]queuedEvent, 0, bufferSize),
		capacity:             bufferSize,
		previewDropThreshold: threshold,
	}
	e.notEmpty = sync.NewCond(&e.mu)
	e.notFull = sync.NewCond(&e.mu)
	return e
}

// Start begins the background writer goroutine.
// Must be called before Emit().
func (e *Emitter) Start() {
	e.wg.Add(1)
	go e.writeLoop()
}

// writeLoop is the background goroutine that writes events.
func (e *Emitter) writeLoop() {
	defer e.wg.Done()

	for {
		e.mu.Lock()
		for len(e.queue) == 0 && !e.closed {
			e.notEmpty.Wait()
		}
		if len(e.queue) == 0 && e.closed {
			e.mu.Unlock()
			return
		}

		evt := e.queue[0]
		copy(e.queue, e.queue[1:])
		e.queue[len(e.queue)-1] = queuedEvent{}
		e.queue = e.queue[:len(e.queue)-1]
		e.notFull.Signal()
		e.mu.Unlock()

		// Write to output (ignore errors - we're best-effort for events)
		_, _ = e.writer.Write(evt.data)
		if evt.done != nil {
			close(evt.done)
		}
	}
}

// Emit serializes and queues an event for writing.
// Non-blocking: if the queue is full, the event is dropped.
// Returns true if the event was queued, false if dropped.
func (e *Emitter) Emit(evt any) bool {
	kind, previewable := classifyEvent(evt)
	if previewable && e.shouldDropPreview() {
		evt = stripPreview(evt)
	}

	data, err := event.SerializeEvent(evt)
	if err != nil {
		// Serialization failed - drop the event
		return false
	}

	return e.enqueue(data, kind)
}

// EmitSync serializes and queues an event, then waits until it is written.
func (e *Emitter) EmitSync(evt any) bool {
	kind, previewable := classifyEvent(evt)
	overloaded := e.shouldDropPreview()
	if previewable && overloaded {
		evt = stripPreview(evt)
	}

	data, err := event.SerializeEvent(evt)
	if err != nil {
		return false
	}

	if kind == eventKindDecision {
		return e.enqueue(data, kind)
	}

	return e.enqueueSync(data, kind)
}

// EmitRaw queues pre-serialized event data for writing.
// Useful for testing or when event is already serialized.
func (e *Emitter) EmitRaw(data []byte) bool {
	return e.enqueue(data, eventKindNormal)
}

// Close signals the emitter to stop and waits for pending events to drain.
func (e *Emitter) Close() {
	e.mu.Lock()
	e.closed = true
	e.notEmpty.Broadcast()
	e.notFull.Broadcast()
	e.mu.Unlock()
	e.wg.Wait()
}

// QueueLength returns the current number of pending events.
// Useful for testing and monitoring.
func (e *Emitter) QueueLength() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.queue)
}

func (e *Emitter) shouldDropPreview() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.queue) >= e.previewDropThreshold
}

func (e *Emitter) enqueue(data []byte, kind eventKind) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return false
	}

	if len(e.queue) >= e.capacity {
		if (kind == eventKindDecision || kind == eventKindPreview) && e.evictPreviewLocked() {
			// Make space by dropping a preview event.
		} else {
			return false
		}
	}

	e.queue = append(e.queue, queuedEvent{data: data, kind: kind})
	e.notEmpty.Signal()
	return true
}

func (e *Emitter) enqueueSync(data []byte, kind eventKind) bool {
	done := make(chan struct{})
	e.mu.Lock()
	for {
		if e.closed {
			e.mu.Unlock()
			return false
		}

		if len(e.queue) < e.capacity {
			e.queue = append(e.queue, queuedEvent{data: data, kind: kind, done: done})
			e.notEmpty.Signal()
			e.mu.Unlock()
			<-done
			return true
		}

		if (kind == eventKindDecision || kind == eventKindPreview) && e.evictPreviewLocked() {
			continue
		}

		e.notFull.Wait()
	}
}

func (e *Emitter) evictPreviewLocked() bool {
	for i, evt := range e.queue {
		if evt.kind != eventKindPreview || evt.done != nil {
			continue
		}
		copy(e.queue[i:], e.queue[i+1:])
		e.queue[len(e.queue)-1] = queuedEvent{}
		e.queue = e.queue[:len(e.queue)-1]
		return true
	}
	return false
}

func classifyEvent(evt any) (eventKind, bool) {
	switch evt.(type) {
	case event.ToolCallDecisionEvent, *event.ToolCallDecisionEvent:
		return eventKindDecision, false
	case event.ToolCallStartEvent, *event.ToolCallStartEvent:
		return eventKindPreview, true
	case event.ToolCallEndEvent, *event.ToolCallEndEvent:
		return eventKindPreview, true
	default:
		return eventKindNormal, false
	}
}

func stripPreview(evt any) any {
	switch e := evt.(type) {
	case event.ToolCallStartEvent:
		e.Call.Preview.Truncated = true
		e.Call.Preview.ArgsPreview = ""
		return e
	case *event.ToolCallStartEvent:
		if e == nil {
			return evt
		}
		copy := *e
		copy.Call.Preview.Truncated = true
		copy.Call.Preview.ArgsPreview = ""
		return copy
	case event.ToolCallEndEvent:
		e.Preview.Truncated = true
		e.Preview.ResultPreview = ""
		return e
	case *event.ToolCallEndEvent:
		if e == nil {
			return evt
		}
		copy := *e
		copy.Preview.Truncated = true
		copy.Preview.ResultPreview = ""
		return copy
	default:
		return evt
	}
}
