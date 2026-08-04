package openai

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

// ResponsesClient streams from an OpenAI-compatible /responses
// endpoint. Reasoning models (o-series, gpt-5.x) require this API
// for function tools; /chat/completions rejects them when reasoning
// is in play. The transcript is still owned by the kernel: requests
// are stateless (store:false) and carry the full input each time.
type ResponsesClient struct {
	BaseURL string
	APIKey  string
	Model   string

	// Effort sets reasoning effort ("low", "medium", "high").
	Effort string
	// Summary requests reasoning summaries ("auto" recommended) so
	// consumers can render the model's deliberation as thinking
	// events. Both empty omits the reasoning parameter entirely.
	Summary string

	HTTPClient *http.Client
}

// wire types — the /responses JSON shapes. Items are polymorphic:
// role messages carry content blocks; function calls and their
// outputs are top-level items.

type respContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type respItem struct {
	Type    string        `json:"type,omitempty"`
	Role    string        `json:"role,omitempty"`
	Content []respContent `json:"content,omitempty"`

	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type respTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type respRequest struct {
	Model        string     `json:"model"`
	Instructions string     `json:"instructions,omitempty"`
	Input        []respItem `json:"input"`
	Tools        []respTool `json:"tools,omitempty"`
	Stream       bool       `json:"stream"`
	Store        bool       `json:"store"`
	Reasoning    *struct {
		Effort  string `json:"effort,omitempty"`
		Summary string `json:"summary,omitempty"`
	} `json:"reasoning,omitempty"`
}

// respEvent is the union of the streamed event payloads we consume.
type respEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta"`
	Item  *struct {
		Type      string `json:"type"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"item"`
	Response *struct {
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
	Message string `json:"message"` // top-level error events
}

// Stream implements agentkit.Streamer.
func (c *ResponsesClient) Stream(ctx context.Context, req agentkit.Request, emit func(agentkit.StreamEvent) error) (agentkit.AssistantMessage, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return agentkit.AssistantMessage{}, fmt.Errorf("openai responses: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return agentkit.AssistantMessage{}, fmt.Errorf("openai responses: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
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
		return agentkit.AssistantMessage{}, agentkit.Transient{Err: fmt.Errorf("openai responses: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return agentkit.AssistantMessage{}, statusError(resp.StatusCode, resp.Body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") &&
		!strings.Contains(ct, "application/json") {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return agentkit.AssistantMessage{}, fmt.Errorf(
			"openai responses: endpoint returned %q, not an event stream — check the base URL (got: %.120s)",
			ct, strings.TrimSpace(string(excerpt)))
	}
	return c.consume(ctx, resp.Body, emit)
}

func (c *ResponsesClient) buildRequest(req agentkit.Request) respRequest {
	w := respRequest{Model: c.Model, Instructions: req.System, Stream: true, Store: false}
	if c.Effort != "" || c.Summary != "" {
		w.Reasoning = &struct {
			Effort  string `json:"effort,omitempty"`
			Summary string `json:"summary,omitempty"`
		}{Effort: c.Effort, Summary: c.Summary}
	}
	for _, m := range req.Messages {
		switch m.Role {
		case agentkit.RoleUser:
			w.Input = append(w.Input, respItem{
				Role: "user", Content: []respContent{{Type: "input_text", Text: m.Text}},
			})
		case agentkit.RoleAssistant:
			if m.Text != "" {
				w.Input = append(w.Input, respItem{
					Role: "assistant", Content: []respContent{{Type: "output_text", Text: m.Text}},
				})
			}
			for _, tc := range m.ToolCalls {
				args := string(tc.Args)
				if tc.ParseErr != "" {
					args = tc.RawArgs // round-trip what the model produced
				}
				w.Input = append(w.Input, respItem{
					Type: "function_call", CallID: tc.ID, Name: tc.Name, Arguments: args,
				})
			}
		case agentkit.RoleTool:
			w.Input = append(w.Input, respItem{
				Type: "function_call_output", CallID: m.ToolCallID, Output: m.Text,
			})
		case agentkit.RoleSystem:
			// System text travels in Instructions; skip stray entries.
		}
	}
	for _, t := range req.Tools {
		w.Tools = append(w.Tools, respTool{
			Type: "function", Name: t.Name, Description: t.Description, Parameters: t.Schema,
		})
	}
	return w
}

func (c *ResponsesClient) consume(ctx context.Context, body io.Reader, emit func(agentkit.StreamEvent) error) (agentkit.AssistantMessage, error) {
	var (
		text     strings.Builder
		calls    []*pendingCall
		byCallID = map[string]*pendingCall{}
		status   string
		reason   string
		usage    agentkit.Usage
	)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ev respEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch {
		case ev.Type == "error":
			return agentkit.AssistantMessage{}, fmt.Errorf("openai responses: stream error: %s", ev.Message)

		case ev.Type == "response.output_text.delta" && ev.Delta != "":
			text.WriteString(ev.Delta)
			if err := emit(agentkit.StreamEvent{Type: agentkit.StreamText, Text: ev.Delta}); err != nil {
				return agentkit.AssistantMessage{}, err
			}

		// Two names for the same product surface: OpenAI streams a
		// summary of its hidden reasoning, DeepSeek streams the raw
		// chain-of-thought and generates no summary at all (it accepts
		// and ignores reasoning.summary). Render whichever arrives.
		case (ev.Type == "response.reasoning_summary_text.delta" ||
			ev.Type == "response.reasoning_text.delta") && ev.Delta != "":
			if err := emit(agentkit.StreamEvent{Type: agentkit.StreamThinking, Text: ev.Delta}); err != nil {
				return agentkit.AssistantMessage{}, err
			}

		case ev.Type == "response.output_item.added" && ev.Item != nil && ev.Item.Type == "function_call":
			pc := &pendingCall{id: ev.Item.CallID}
			pc.name = ev.Item.Name
			byCallID[ev.Item.CallID] = pc
			calls = append(calls, pc)
			call := agentkit.ToolCall{ID: pc.id, Name: pc.name}
			if err := emit(agentkit.StreamEvent{Type: agentkit.StreamToolCall, Call: &call}); err != nil {
				return agentkit.AssistantMessage{}, err
			}

		case ev.Type == "response.output_item.done" && ev.Item != nil && ev.Item.Type == "function_call":
			// Authoritative arguments arrive on the completed item.
			pc, ok := byCallID[ev.Item.CallID]
			if !ok {
				pc = &pendingCall{id: ev.Item.CallID, name: ev.Item.Name}
				calls = append(calls, pc)
				byCallID[ev.Item.CallID] = pc
			}
			pc.args.Reset()
			pc.args.WriteString(ev.Item.Arguments)

		case ev.Type == "response.completed" || ev.Type == "response.incomplete" || ev.Type == "response.failed":
			if ev.Response != nil {
				status = ev.Response.Status
				if ev.Response.IncompleteDetails != nil {
					reason = ev.Response.IncompleteDetails.Reason
				}
				if ev.Response.Usage != nil {
					usage.InputTokens = ev.Response.Usage.InputTokens
					usage.OutputTokens = ev.Response.Usage.OutputTokens
				}
				if ev.Response.Error != nil {
					return agentkit.AssistantMessage{}, fmt.Errorf("openai responses: %s", ev.Response.Error.Message)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return agentkit.AssistantMessage{}, ctx.Err()
		}
		return agentkit.AssistantMessage{}, agentkit.Transient{Err: fmt.Errorf("openai responses: stream read: %w", err)}
	}
	if status == "" {
		return agentkit.AssistantMessage{}, agentkit.Transient{Err: errors.New("openai responses: stream ended without completion")}
	}

	msg := agentkit.AssistantMessage{Text: text.String(), Usage: usage}
	for _, pc := range calls {
		msg.ToolCalls = append(msg.ToolCalls, pc.toolCall())
	}
	switch {
	case status == "incomplete" && reason == "max_output_tokens":
		msg.StopReason = agentkit.StopLength
	case len(msg.ToolCalls) > 0:
		msg.StopReason = agentkit.StopToolCalls
	default:
		msg.StopReason = agentkit.StopEnd
	}
	return msg, nil
}
