// Package anthropic implements agentkit.Streamer over the Anthropic
// Messages API (api.anthropic.com and native-protocol gateways).
// Model, endpoint, and credentials are plain client configuration —
// resolving them (flags, config files, env) is the application's job.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

// Client streams from one Anthropic Messages endpoint.
type Client struct {
	BaseURL string // e.g. "https://api.anthropic.com"; "/v1/messages" is appended
	APIKey  string
	Model   string

	// MaxTokens caps one response; the Messages API requires it.
	// Zero means 8192 (raised above ThinkingBudget when manual
	// thinking is on, since the budget counts against max_tokens).
	MaxTokens int

	// The thinking wire shape is generational — set the field that
	// matches the model (LegacyThinking reports which):
	//
	// ThinkingBudget enables manual extended thinking with that token
	// budget (API minimum 1024) via thinking:{enabled,budget_tokens}.
	// That shape is the ONLY mode on Claude 4.5-and-earlier models,
	// deprecated on 4.6, and a 400 on 4.7 and later.
	ThinkingBudget int

	// Effort sets output_config:{effort} ("low"/"medium"/"high"/
	// "xhigh"/"max") on Claude 4.6-and-later models, where it is the
	// depth control and thinking is model-decided (adaptive) by
	// default. Empty omits the parameter (the API default is high).
	// Legacy models reject output_config — do not combine with
	// ThinkingBudget.
	Effort string

	// Whichever mode applies, thinking blocks are emitted as
	// StreamThinking and round-tripped verbatim through
	// Message.Provider — the API rejects tool-use turns whose
	// thinking blocks were dropped or altered.

	// HTTPClient defaults to a client with no overall timeout:
	// streaming responses are long-lived; cancel via ctx.
	HTTPClient *http.Client
}

// wire types — the Messages API JSON shapes. Content blocks we
// produce are typed; blocks we round-trip (thinking) stay raw.

type wireBlock struct {
	Type string `json:"type"`

	Text string `json:"text,omitempty"` // text

	ID    string          `json:"id,omitempty"`    // tool_use
	Name  string          `json:"name,omitempty"`  // tool_use
	Input json.RawMessage `json:"input,omitempty"` // tool_use

	ToolUseID string `json:"tool_use_id,omitempty"` // tool_result
	Content   string `json:"content,omitempty"`     // tool_result
	IsError   bool   `json:"is_error,omitempty"`    // tool_result
}

type wireMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type wireTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type wireRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	System    string        `json:"system,omitempty"`
	Messages  []wireMessage `json:"messages"`
	Tools     []wireTool    `json:"tools,omitempty"`
	Stream    bool          `json:"stream"`
	Thinking  *struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens"`
	} `json:"thinking,omitempty"`
	OutputConfig *struct {
		Effort string `json:"effort"`
	} `json:"output_config,omitempty"`
}

// LegacyThinking reports whether a model predates the Claude 4.6
// generation and therefore takes manual thinking:{enabled,
// budget_tokens} (ThinkingBudget) rather than output_config effort
// (Effort). 4.7-and-later models return a 400 for the manual shape;
// 4.5-and-earlier models return a 400 for output_config. Unversioned
// aliases are assumed current.
func LegacyThinking(model string) bool {
	m := strings.ToLower(model)
	if !strings.Contains(m, "claude") {
		return false
	}
	// Version tokens: first small integer is the major, an optional
	// following small integer is the minor ("claude-opus-4-5",
	// "claude-3-7-sonnet-20250219", "claude-opus-5"). Large numbers
	// are date stamps, not versions.
	major, minor := -1, 0
	for _, tok := range strings.Split(m, "-") {
		n := 0
		numeric := tok != ""
		for _, r := range tok {
			if r < '0' || r > '9' {
				numeric = false
				break
			}
			n = n*10 + int(r-'0')
		}
		if !numeric || n >= 100 {
			continue
		}
		if major < 0 {
			major = n
			continue
		}
		minor = n
		break
	}
	if major < 0 {
		return false
	}
	return major*10+minor < 46
}

// wireEvent is the union of the streamed event payloads we consume.
type wireEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`

	Message *struct {
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"`

	ContentBlock json.RawMessage `json:"content_block"`

	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`

	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`

	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Stream implements agentkit.Streamer.
func (c *Client) Stream(ctx context.Context, req agentkit.Request, emit func(agentkit.StreamEvent) error) (agentkit.AssistantMessage, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return agentkit.AssistantMessage{}, fmt.Errorf("anthropic: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return agentkit.AssistantMessage{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Accept", "text/event-stream")

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 0}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return agentkit.AssistantMessage{}, ctx.Err()
		}
		return agentkit.AssistantMessage{}, agentkit.Transient{Err: fmt.Errorf("anthropic: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return agentkit.AssistantMessage{}, statusError(resp.StatusCode, resp.Body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") &&
		!strings.Contains(ct, "application/json") {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return agentkit.AssistantMessage{}, fmt.Errorf(
			"anthropic: endpoint returned %q, not an event stream — check the base URL (got: %.120s)",
			ct, strings.TrimSpace(string(excerpt)))
	}
	return c.consume(ctx, resp.Body, emit)
}

// endpoint appends the Messages path, tolerating bases that already
// include /v1 (the OpenAI-compat convention this codebase uses).
func (c *Client) endpoint() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

func (c *Client) buildRequest(req agentkit.Request) wireRequest {
	maxTokens := c.MaxTokens
	if maxTokens == 0 {
		maxTokens = 8192
	}
	w := wireRequest{Model: c.Model, System: req.System, Stream: true, Messages: []wireMessage{}}
	if c.ThinkingBudget > 0 {
		budget := c.ThinkingBudget
		if budget < 1024 {
			budget = 1024
		}
		// The budget counts against max_tokens; keep headroom for the
		// visible answer.
		if maxTokens <= budget {
			maxTokens = budget + 8192
		}
		w.Thinking = &struct {
			Type         string `json:"type"`
			BudgetTokens int    `json:"budget_tokens"`
		}{Type: "enabled", BudgetTokens: budget}
	}
	if c.Effort != "" {
		w.OutputConfig = &struct {
			Effort string `json:"effort"`
		}{Effort: c.Effort}
	}
	w.MaxTokens = maxTokens

	for _, m := range req.Messages {
		role, blocks := blocksFor(m)
		if len(blocks) == 0 {
			continue
		}
		// The API wants tool results in the next user turn and rejects
		// some same-role sequences; coalescing adjacent same-role
		// messages satisfies both.
		if n := len(w.Messages); n > 0 && w.Messages[n-1].Role == role {
			w.Messages[n-1].Content = append(w.Messages[n-1].Content, blocks...)
			continue
		}
		w.Messages = append(w.Messages, wireMessage{Role: role, Content: blocks})
	}
	for _, t := range req.Tools {
		w.Tools = append(w.Tools, wireTool{Name: t.Name, Description: t.Description, InputSchema: t.Schema})
	}
	return w
}

// blocksFor maps one transcript message to wire content blocks.
func blocksFor(m agentkit.Message) (role string, blocks []json.RawMessage) {
	raw := func(b wireBlock) json.RawMessage {
		out, _ := json.Marshal(b)
		return out
	}
	switch m.Role {
	case agentkit.RoleUser:
		if m.Text != "" {
			blocks = append(blocks, raw(wireBlock{Type: "text", Text: m.Text}))
		}
		return "user", blocks
	case agentkit.RoleAssistant:
		// Thinking blocks first, verbatim — signatures are validated
		// and the API requires the turn to open with thinking when it
		// thought. Assumption: putting ALL thinking blocks before
		// text/tool_use preserves a valid order because relative
		// thinking order is kept and the API validates the turn's
		// opening block, not interior interleaving.
		var provider []json.RawMessage
		if len(m.Provider) > 0 && json.Unmarshal(m.Provider, &provider) == nil {
			blocks = append(blocks, provider...)
		}
		if m.Text != "" {
			blocks = append(blocks, raw(wireBlock{Type: "text", Text: m.Text}))
		}
		for _, tc := range m.ToolCalls {
			input := json.RawMessage(tc.Args)
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			blocks = append(blocks, raw(wireBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input}))
		}
		return "assistant", blocks
	case agentkit.RoleTool:
		return "user", []json.RawMessage{raw(wireBlock{
			Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Text, IsError: m.IsError,
		})}
	default: // system text travels in the request's System field
		return "", nil
	}
}

// consume reads the SSE stream, emitting deltas and assembling the
// final message. Blocks are tracked by stream index; thinking and
// redacted_thinking blocks are reassembled raw for round-trip.
func (c *Client) consume(ctx context.Context, body io.Reader, emit func(agentkit.StreamEvent) error) (agentkit.AssistantMessage, error) {
	var (
		text       strings.Builder
		stopReason string
		usage      agentkit.Usage
		calls      []*pendingCall
		thinking   []json.RawMessage
		open       = map[int]*openBlock{}
		done       = false
	)

	sawEvent := false
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev wireEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev); err != nil {
			continue // tolerate keepalives
		}
		sawEvent = true
		switch ev.Type {
		case "error":
			if ev.Error == nil {
				return agentkit.AssistantMessage{}, errors.New("anthropic: stream error")
			}
			err := fmt.Errorf("anthropic: stream error: %s: %s", ev.Error.Type, ev.Error.Message)
			if ev.Error.Type == "overloaded_error" || ev.Error.Type == "api_error" {
				return agentkit.AssistantMessage{}, agentkit.Transient{Err: err}
			}
			return agentkit.AssistantMessage{}, err

		case "message_start":
			if ev.Message != nil {
				usage.InputTokens = ev.Message.Usage.InputTokens
			}

		case "content_block_start":
			ob := &openBlock{}
			var head struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			_ = json.Unmarshal(ev.ContentBlock, &head)
			ob.kind = head.Type
			switch head.Type {
			case "tool_use":
				ob.call = &pendingCall{id: head.ID, name: head.Name}
				calls = append(calls, ob.call)
				call := agentkit.ToolCall{ID: head.ID, Name: head.Name}
				if err := emit(agentkit.StreamEvent{Type: agentkit.StreamToolCall, Call: &call}); err != nil {
					return agentkit.AssistantMessage{}, err
				}
			case "redacted_thinking":
				// Arrives complete; keep the whole block verbatim.
				thinking = append(thinking, append(json.RawMessage(nil), ev.ContentBlock...))
			}
			open[ev.Index] = ob

		case "content_block_delta":
			ob := open[ev.Index]
			if ob == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				text.WriteString(ev.Delta.Text)
				if err := emit(agentkit.StreamEvent{Type: agentkit.StreamText, Text: ev.Delta.Text}); err != nil {
					return agentkit.AssistantMessage{}, err
				}
			case "thinking_delta":
				ob.thinking.WriteString(ev.Delta.Thinking)
				if err := emit(agentkit.StreamEvent{Type: agentkit.StreamThinking, Text: ev.Delta.Thinking}); err != nil {
					return agentkit.AssistantMessage{}, err
				}
			case "signature_delta":
				ob.signature += ev.Delta.Signature
			case "input_json_delta":
				if ob.call != nil {
					ob.call.args.WriteString(ev.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			ob := open[ev.Index]
			if ob != nil && ob.kind == "thinking" {
				block, _ := json.Marshal(map[string]string{
					"type": "thinking", "thinking": ob.thinking.String(), "signature": ob.signature,
				})
				thinking = append(thinking, block)
			}
			delete(open, ev.Index)

		case "message_delta":
			if ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Usage != nil {
				usage.OutputTokens = ev.Usage.OutputTokens
			}

		case "message_stop":
			done = true
		}
		if done {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return agentkit.AssistantMessage{}, ctx.Err()
		}
		return agentkit.AssistantMessage{}, agentkit.Transient{Err: fmt.Errorf("anthropic: stream read: %w", err)}
	}
	if !done {
		if !sawEvent {
			// A clean 200 with zero stream events is not a hiccup —
			// it is an endpoint that did not stream (wrong path, a
			// gateway that ignored stream:true). Retrying won't help.
			return agentkit.AssistantMessage{}, errors.New(
				"anthropic: endpoint sent no stream events — check the base URL speaks the Messages API")
		}
		return agentkit.AssistantMessage{}, agentkit.Transient{Err: errors.New("anthropic: stream ended without completion")}
	}

	msg := agentkit.AssistantMessage{Text: text.String(), Usage: usage}
	for _, pc := range calls {
		msg.ToolCalls = append(msg.ToolCalls, pc.toolCall())
	}
	if len(thinking) > 0 {
		msg.Provider, _ = json.Marshal(thinking)
	}
	switch stopReason {
	case "max_tokens":
		msg.StopReason = agentkit.StopLength
	case "tool_use":
		msg.StopReason = agentkit.StopToolCalls
	case "refusal":
		// A safety refusal arrives as HTTP 200 with a stop_reason;
		// surfacing it as a normal completion would fake success.
		return agentkit.AssistantMessage{}, errors.New(
			"anthropic: the model declined to continue this request (stop_reason: refusal)")
	default:
		// end_turn, stop_sequence — and pause_turn would land here
		// too, but only server tools produce it and we define none.
		msg.StopReason = agentkit.StopEnd
	}
	return msg, nil
}

// openBlock tracks one in-flight content block by stream index.
type openBlock struct {
	kind      string
	call      *pendingCall
	thinking  strings.Builder
	signature string
}

type pendingCall struct {
	id   string
	name string
	args strings.Builder
}

// toolCall validates accumulated input. Malformed JSON is preserved
// verbatim in RawArgs with the parse diagnostic; the loop never
// executes a call with ParseErr set.
func (p *pendingCall) toolCall() agentkit.ToolCall {
	raw := p.args.String()
	call := agentkit.ToolCall{ID: p.id, Name: p.name}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		call.Args = json.RawMessage("{}")
		return call
	}
	var probe any
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		call.Args = json.RawMessage("{}")
		call.RawArgs = raw
		call.ParseErr = err.Error()
		return call
	}
	call.Args = json.RawMessage(trimmed)
	return call
}

// APIError is a non-200 response from the endpoint, exposed as a
// type so applications can classify protocol misroutes (e.g. a
// gateway with no /v1/messages) without parsing flattened strings.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("anthropic: status %d: %s", e.Status, e.Body)
}

func statusError(status int, body io.Reader) error {
	excerpt, _ := io.ReadAll(io.LimitReader(body, 2048))
	err := &APIError{Status: status, Body: strings.TrimSpace(string(excerpt))}
	// 529 is Anthropic's "overloaded"; treat like 5xx.
	if status == http.StatusTooManyRequests || status >= 500 {
		return agentkit.Transient{Err: err}
	}
	return err
}
