// Package agentkit is a minimal embeddable agent kernel: a
// provider-neutral loop, typed tools, a pre-execution gate, hard
// limits, and explicit skill invocation. It is stdlib-only and holds
// no sessions, no config, no renderer, and no domain logic — those
// belong to the embedding application.
package agentkit

import "encoding/json"

// Role identifies who produced a transcript message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// StopReason reports why the model stopped generating.
type StopReason string

const (
	StopEnd       StopReason = "stop"       // natural completion
	StopToolCalls StopReason = "tool_calls" // stopped to call tools
	StopLength    StopReason = "length"     // cut off by token limit
)

// ToolCall is a tool invocation requested by the model.
//
// When the model emits arguments that do not parse as JSON, Args is
// set to "{}" so the transcript stays serializable, and the original
// text and diagnostic are preserved in RawArgs / ParseErr. The loop
// never executes a call whose ParseErr is non-empty.
type ToolCall struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"args"`
	RawArgs  string          `json:"rawArgs,omitempty"`
	ParseErr string          `json:"parseErr,omitempty"`
}

// Message is one provider-neutral transcript entry. Fields are
// role-dependent: Text for system/user/assistant, ToolCalls for
// assistant, ToolCallID/ToolName/IsError for tool results.
type Message struct {
	Role      Role       `json:"role"`
	Text      string     `json:"text,omitempty"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`

	ToolCallID string `json:"toolCallId,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	IsError    bool   `json:"isError,omitempty"`

	// Provider is opaque continuity data owned by the Streamer that
	// produced the message (e.g. Anthropic extended-thinking blocks,
	// whose signatures must round-trip verbatim). Only that client
	// reads it back; the kernel, tools, and renderers never interpret
	// it, and other providers ignore it.
	Provider json.RawMessage `json:"provider,omitempty"`
}

// Usage counts tokens for one model request.
type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// AssistantMessage is the assembled result of one model request.
type AssistantMessage struct {
	Text       string
	ToolCalls  []ToolCall
	StopReason StopReason
	Usage      Usage
	Provider   json.RawMessage // see Message.Provider
}

// Message converts the assembled result to a transcript entry.
func (a AssistantMessage) Message() Message {
	return Message{Role: RoleAssistant, Text: a.Text, ToolCalls: a.ToolCalls, Provider: a.Provider}
}
