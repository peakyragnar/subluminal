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
	// Events are dropped if queue is full (non-blocking emit).
	DefaultBufferSize = 1000
)

// Emitter handles async event emission to a writer (typically stderr).
type Emitter struct {
	writer io.Writer
	events chan queuedEvent
	done   chan struct{}
	wg     sync.WaitGroup
}

type queuedEvent struct {
	data []byte
	done chan struct{}
}

// NewEmitter creates a new Emitter that writes to the given writer.
func NewEmitter(w io.Writer) *Emitter {
	return &Emitter{
		writer: w,
		events: make(chan queuedEvent, DefaultBufferSize),
		done:   make(chan struct{}),
	}
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
		select {
		case evt := <-e.events:
			// Write to output (ignore errors - we're best-effort for events)
			_, _ = e.writer.Write(evt.data)
			if evt.done != nil {
				close(evt.done)
			}
		case <-e.done:
			// Drain remaining events before exiting
			for {
				select {
				case evt := <-e.events:
					_, _ = e.writer.Write(evt.data)
					if evt.done != nil {
						close(evt.done)
					}
				default:
					return
				}
			}
		}
	}
}

// Emit serializes and queues an event for writing.
// Non-blocking: if the queue is full, the event is dropped.
// Returns true if the event was queued, false if dropped.
func (e *Emitter) Emit(evt any) bool {
	data, err := event.SerializeEvent(evt)
	if err != nil {
		// Serialization failed - drop the event
		return false
	}

	select {
	case e.events <- queuedEvent{data: data}:
		return true
	default:
		// Queue full - drop the event
		return false
	}
}

// EmitSync serializes and queues an event, then waits until it is written.
func (e *Emitter) EmitSync(evt any) bool {
	data, err := event.SerializeEvent(evt)
	if err != nil {
		return false
	}

	done := make(chan struct{})
	select {
	case e.events <- queuedEvent{data: data, done: done}:
		<-done
		return true
	case <-e.done:
		return false
	}
}

// EmitRaw queues pre-serialized event data for writing.
// Useful for testing or when event is already serialized.
func (e *Emitter) EmitRaw(data []byte) bool {
	select {
	case e.events <- queuedEvent{data: data}:
		return true
	default:
		return false
	}
}

// Close signals the emitter to stop and waits for pending events to drain.
func (e *Emitter) Close() {
	close(e.done)
	e.wg.Wait()
}

// QueueLength returns the current number of pending events.
// Useful for testing and monitoring.
func (e *Emitter) QueueLength() int {
	return len(e.events)
}
