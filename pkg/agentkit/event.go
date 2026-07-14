package agentkit

import "encoding/json"

// Event is one agent lifecycle event. Consumers type-switch on the
// concrete types below. Model-stream events arrive wrapped in
// ModelEvent — the two vocabularies are layered, not merged.
type Event interface{ event() }

// RunStart begins a run.
type RunStart struct{}

// TurnStart begins turn N (1-based). One turn is one assistant
// response plus its tool calls and results.
type TurnStart struct{ Turn int }

// ModelEvent wraps one model-stream event received during a turn.
type ModelEvent struct {
	Turn   int
	Stream StreamEvent
}

// ToolStart precedes execution of an allowed tool call.
type ToolStart struct {
	Turn int
	Call ToolCall
}

// ToolEnd reports the outcome of one tool call: executed (Result or
// Err), denied by the gate (Denied + Err), or rejected before the
// gate (invalid name/args, truncation — Err only).
type ToolEnd struct {
	Turn   int
	Call   ToolCall
	Result json.RawMessage
	Err    string
	Denied bool
}

// TurnEnd completes a turn with the assembled assistant message.
type TurnEnd struct {
	Turn    int
	Message AssistantMessage
}

// RunEnd is the final event of a run.
type RunEnd struct {
	// Reason: "done" (model finished without tool calls), or the
	// run-terminating error string.
	Reason string
	Usage  Usage // summed across all model requests
}

func (RunStart) event()   {}
func (TurnStart) event()  {}
func (ModelEvent) event() {}
func (ToolStart) event()  {}
func (ToolEnd) event()    {}
func (TurnEnd) event()    {}
func (RunEnd) event()     {}
