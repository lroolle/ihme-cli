package agentkit

import (
	"context"
	"errors"
)

// Request is one provider-neutral model request. The model name,
// endpoint, and credentials are the Streamer's own configuration —
// the kernel does not choose providers.
type Request struct {
	System   string
	Messages []Message
	Tools    []ToolDef
}

// ToolDef is the model-visible description of a tool.
type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
}

// StreamEventType distinguishes model-stream events. This is the
// model-stream vocabulary; agent lifecycle events (event.go) carry
// these as payload rather than sharing one flat vocabulary.
type StreamEventType string

const (
	StreamText     StreamEventType = "text"      // Text holds a delta
	StreamToolCall StreamEventType = "tool_call" // Call assembled
	StreamThinking StreamEventType = "thinking"  // Text holds a delta
)

// StreamEvent is one model-stream event.
type StreamEvent struct {
	Type StreamEventType
	Text string
	Call *ToolCall
}

// Streamer produces one assistant message per call, emitting stream
// events synchronously as they arrive. Emit errors must abort the
// request and propagate: the callback is the backpressure and
// cancellation path for renderers. The returned message is the
// authoritative assembled result; events are for observation only.
type Streamer interface {
	Stream(ctx context.Context, req Request, emit func(StreamEvent) error) (AssistantMessage, error)
}

// Transient wraps an error a backend judged retryable (connection
// resets, 429s, 5xx). The loop retries a request only when the error
// unwraps to Transient AND no meaningful stream output was emitted;
// everything else fails the run. Backends decide what is transient —
// the kernel only honors the marker.
type Transient struct{ Err error }

func (t Transient) Error() string { return "transient: " + t.Err.Error() }
func (t Transient) Unwrap() error { return t.Err }

// IsTransient reports whether err unwraps to a Transient marker.
func IsTransient(err error) bool {
	var t Transient
	return errors.As(err, &t)
}
