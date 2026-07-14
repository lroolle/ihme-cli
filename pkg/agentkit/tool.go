package agentkit

import (
	"context"
	"encoding/json"
)

// Tool is an executable capability exposed to the model. Timeout and
// cancellation flow through ctx. Execute returns the tool result as
// JSON text for the model; an error becomes a model-visible tool
// error result. The loop never retries Execute — tools that mutate
// state rely on that.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}

// FuncTool adapts a function to the Tool interface.
type FuncTool struct {
	ToolName string
	Desc     string
	Params   map[string]any
	Fn       func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}

func (t FuncTool) Name() string           { return t.ToolName }
func (t FuncTool) Description() string    { return t.Desc }
func (t FuncTool) Schema() map[string]any { return t.Params }
func (t FuncTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	return t.Fn(ctx, args)
}

// Defs converts tools to their model-visible definitions.
func Defs(tools []Tool) []ToolDef {
	defs := make([]ToolDef, len(tools))
	for i, t := range tools {
		defs[i] = ToolDef{Name: t.Name(), Description: t.Description(), Schema: t.Schema()}
	}
	return defs
}

// GateRequest is one tool call awaiting permission. Args are already
// validated JSON — the gate never sees malformed input.
type GateRequest struct {
	Turn int
	Call ToolCall
}

// GateDecision allows or denies a tool call. On deny, Reason is
// returned to the model as the tool result so it can adapt.
type GateDecision struct {
	Allowed bool
	Reason  string
}

// Gate decides whether a tool call may execute. It runs after
// argument validation and before execution. Interactive consent
// (prompting a user) is the gate implementation's concern, not the
// kernel's: block inside the gate, then return the decision.
type Gate func(ctx context.Context, req GateRequest) GateDecision
