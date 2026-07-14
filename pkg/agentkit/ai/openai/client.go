// Package openai implements agentkit.Streamer over any
// OpenAI-compatible /chat/completions endpoint (OpenAI, Anthropic's
// and Google's compat endpoints, Ollama, LiteLLM/new-api gateways).
// Model, endpoint, and credentials are plain client configuration —
// resolving them (flags, config files, env) is the application's job.
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

// Client streams chat completions from one OpenAI-compatible endpoint.
type Client struct {
	BaseURL string // e.g. "https://api.example.com/v1"
	APIKey  string
	Model   string

	// HTTPClient defaults to a client with no overall timeout:
	// streaming responses are long-lived; cancel via ctx.
	HTTPClient *http.Client
}

// wire types — the OpenAI chat-completions JSON shapes.

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type wireRequest struct {
	Model         string        `json:"model"`
	Messages      []wireMessage `json:"messages"`
	Tools         []wireTool    `json:"tools,omitempty"`
	Stream        bool          `json:"stream"`
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

type chunkDelta struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	ToolCalls        []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type chunk struct {
	Choices []struct {
		Delta        chunkDelta `json:"delta"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Stream implements agentkit.Streamer.
func (c *Client) Stream(ctx context.Context, req agentkit.Request, emit func(agentkit.StreamEvent) error) (agentkit.AssistantMessage, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return agentkit.AssistantMessage{}, fmt.Errorf("openai: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return agentkit.AssistantMessage{}, fmt.Errorf("openai: build request: %w", err)
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
		// Connection-level failures are transient by nature.
		return agentkit.AssistantMessage{}, agentkit.Transient{Err: fmt.Errorf("openai: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return agentkit.AssistantMessage{}, statusError(resp.StatusCode, resp.Body)
	}
	// A 200 that is not an event stream is a misconfiguration (wrong
	// path, proxy login page) — fail loud and permanent, not transient.
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") &&
		!strings.Contains(ct, "application/json") {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return agentkit.AssistantMessage{}, fmt.Errorf(
			"openai: endpoint returned %q, not an event stream — check the base URL (got: %.120s)",
			ct, strings.TrimSpace(string(excerpt)))
	}
	return c.consume(ctx, resp.Body, emit)
}

func (c *Client) buildRequest(req agentkit.Request) wireRequest {
	w := wireRequest{Model: c.Model, Stream: true}
	w.StreamOptions.IncludeUsage = true
	if req.System != "" {
		w.Messages = append(w.Messages, wireMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role), Content: m.Text}
		switch m.Role {
		case agentkit.RoleAssistant:
			for _, tc := range m.ToolCalls {
				var wtc wireToolCall
				wtc.ID = tc.ID
				wtc.Type = "function"
				wtc.Function.Name = tc.Name
				// Send what the model originally produced: malformed
				// raw text round-trips unchanged in the transcript.
				if tc.ParseErr != "" {
					wtc.Function.Arguments = tc.RawArgs
				} else {
					wtc.Function.Arguments = string(tc.Args)
				}
				wm.ToolCalls = append(wm.ToolCalls, wtc)
			}
		case agentkit.RoleTool:
			wm.ToolCallID = m.ToolCallID
		}
		w.Messages = append(w.Messages, wm)
	}
	for _, t := range req.Tools {
		var wt wireTool
		wt.Type = "function"
		wt.Function.Name = t.Name
		wt.Function.Description = t.Description
		wt.Function.Parameters = t.Schema
		w.Tools = append(w.Tools, wt)
	}
	return w
}

// consume reads the SSE stream, emitting deltas and assembling the
// final message.
func (c *Client) consume(ctx context.Context, body io.Reader, emit func(agentkit.StreamEvent) error) (agentkit.AssistantMessage, error) {
	var (
		text       strings.Builder
		finish     string
		usage      agentkit.Usage
		calls      []*pendingCall
		callsByIdx = map[int]*pendingCall{}
	)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	sawDone := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawDone = true
			break
		}
		var ck chunk
		if err := json.Unmarshal([]byte(data), &ck); err != nil {
			continue // tolerate non-JSON keepalives
		}
		if ck.Error != nil {
			return agentkit.AssistantMessage{}, fmt.Errorf("openai: stream error: %s", ck.Error.Message)
		}
		if ck.Usage != nil {
			usage.InputTokens = ck.Usage.PromptTokens
			usage.OutputTokens = ck.Usage.CompletionTokens
		}
		if len(ck.Choices) == 0 {
			continue
		}
		choice := ck.Choices[0]
		if choice.FinishReason != "" {
			finish = choice.FinishReason
		}
		if choice.Delta.Content != "" {
			text.WriteString(choice.Delta.Content)
			if err := emit(agentkit.StreamEvent{Type: agentkit.StreamText, Text: choice.Delta.Content}); err != nil {
				return agentkit.AssistantMessage{}, err
			}
		}
		if choice.Delta.ReasoningContent != "" {
			if err := emit(agentkit.StreamEvent{Type: agentkit.StreamThinking, Text: choice.Delta.ReasoningContent}); err != nil {
				return agentkit.AssistantMessage{}, err
			}
		}
		for _, tc := range choice.Delta.ToolCalls {
			pc, ok := callsByIdx[tc.Index]
			if !ok {
				pc = &pendingCall{}
				callsByIdx[tc.Index] = pc
				calls = append(calls, pc)
			}
			if tc.ID != "" {
				pc.id = tc.ID
			}
			if tc.Function.Name != "" {
				first := pc.name == ""
				pc.name += tc.Function.Name
				if first {
					// Announce the call as soon as it has a name so
					// renderers show progress before args finish.
					call := agentkit.ToolCall{ID: pc.id, Name: pc.name}
					if err := emit(agentkit.StreamEvent{Type: agentkit.StreamToolCall, Call: &call}); err != nil {
						return agentkit.AssistantMessage{}, err
					}
				}
			}
			pc.args.WriteString(tc.Function.Arguments)
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return agentkit.AssistantMessage{}, ctx.Err()
		}
		// Mid-stream transport failure: transient in nature; the loop
		// retries only if nothing meaningful was emitted yet.
		return agentkit.AssistantMessage{}, agentkit.Transient{Err: fmt.Errorf("openai: stream read: %w", err)}
	}
	if !sawDone && finish == "" {
		return agentkit.AssistantMessage{}, agentkit.Transient{Err: errors.New("openai: stream ended without completion")}
	}

	msg := agentkit.AssistantMessage{
		Text:       text.String(),
		StopReason: mapFinish(finish),
		Usage:      usage,
	}
	for _, pc := range calls {
		msg.ToolCalls = append(msg.ToolCalls, pc.toolCall())
	}
	return msg, nil
}

type pendingCall struct {
	id   string
	name string
	args strings.Builder
}

// toolCall validates accumulated arguments. Malformed JSON is
// preserved verbatim in RawArgs with the parse diagnostic; Args
// falls back to "{}" only so the transcript stays serializable —
// the loop never executes a call with ParseErr set.
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

func mapFinish(reason string) agentkit.StopReason {
	switch reason {
	case "length":
		return agentkit.StopLength
	case "tool_calls", "function_call":
		return agentkit.StopToolCalls
	default:
		return agentkit.StopEnd
	}
}

func statusError(status int, body io.Reader) error {
	excerpt, _ := io.ReadAll(io.LimitReader(body, 2048))
	err := fmt.Errorf("openai: status %d: %s", status, strings.TrimSpace(string(excerpt)))
	if status == http.StatusTooManyRequests || status >= 500 {
		return agentkit.Transient{Err: err}
	}
	return err
}
