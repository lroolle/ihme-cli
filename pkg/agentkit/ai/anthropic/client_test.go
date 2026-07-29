package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

// sseServer serves scripted SSE lines for one request and captures
// the request body and headers.
func sseServer(t *testing.T, lines []string, capture *[]byte, headers *http.Header) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			body, _ := io.ReadAll(r.Body)
			*capture = body
		}
		if headers != nil {
			*headers = r.Header.Clone()
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range lines {
			_, _ = w.Write([]byte(line + "\n\n"))
		}
	}))
}

func client(url string) *Client {
	return &Client{BaseURL: url, APIKey: "test-key", Model: "claude-test"}
}

func run(t *testing.T, lines []string, req agentkit.Request) (agentkit.AssistantMessage, []agentkit.StreamEvent, error) {
	t.Helper()
	srv := sseServer(t, lines, nil, nil)
	defer srv.Close()
	var events []agentkit.StreamEvent
	msg, err := client(srv.URL).Stream(context.Background(), req, func(ev agentkit.StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	return msg, events, err
}

func TestTextStreaming(t *testing.T) {
	msg, events, err := run(t, []string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":12}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
		`data: {"type":"message_stop"}`,
	}, agentkit.Request{Messages: []agentkit.Message{{Role: agentkit.RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "Hello" || msg.StopReason != agentkit.StopEnd {
		t.Fatalf("msg = %+v", msg)
	}
	if msg.Usage.InputTokens != 12 || msg.Usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v", msg.Usage)
	}
	if len(events) != 2 || events[0].Text != "Hel" {
		t.Fatalf("events = %+v", events)
	}
}

func TestToolUseAssemblyAcrossFragments(t *testing.T) {
	msg, events, err := run(t, []string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c1","name":"search"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"github\"}"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"c2","name":"now"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
		`data: {"type":"message_stop"}`,
	}, agentkit.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != agentkit.StopToolCalls || len(msg.ToolCalls) != 2 {
		t.Fatalf("msg = %+v", msg)
	}
	if string(msg.ToolCalls[0].Args) != `{"q":"github"}` || msg.ToolCalls[0].Name != "search" || msg.ToolCalls[0].ID != "c1" {
		t.Fatalf("call0 = %+v", msg.ToolCalls[0])
	}
	// A tool_use with no input deltas means empty input.
	if string(msg.ToolCalls[1].Args) != "{}" {
		t.Fatalf("call1 = %+v", msg.ToolCalls[1])
	}
	names := []string{}
	for _, ev := range events {
		if ev.Type == agentkit.StreamToolCall {
			names = append(names, ev.Call.Name)
		}
	}
	if strings.Join(names, ",") != "search,now" {
		t.Fatalf("announced calls = %v", names)
	}
}

func TestMalformedInputPreserved(t *testing.T) {
	msg, _, err := run(t, []string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c1","name":"reserve"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"addr\": tru"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
		`data: {"type":"message_stop"}`,
	}, agentkit.Request{})
	if err != nil {
		t.Fatal(err)
	}
	call := msg.ToolCalls[0]
	if call.ParseErr == "" || call.RawArgs != `{"addr": tru` {
		t.Fatalf("diagnostics lost: %+v", call)
	}
	if string(call.Args) != "{}" {
		t.Fatalf("fallback args = %s", call.Args)
	}
}

func TestThinkingBlocksEmittedAndPreserved(t *testing.T) {
	msg, events, err := run(t, []string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"weigh the"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" options"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-abc"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"opaque-bytes"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"text"}}`,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"done"}}`,
		`data: {"type":"content_block_stop","index":2}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`data: {"type":"message_stop"}`,
	}, agentkit.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "done" {
		t.Fatalf("text = %q", msg.Text)
	}
	var thinkingText strings.Builder
	for _, ev := range events {
		if ev.Type == agentkit.StreamThinking {
			thinkingText.WriteString(ev.Text)
		}
	}
	if thinkingText.String() != "weigh the options" {
		t.Fatalf("thinking events = %q", thinkingText.String())
	}
	// The provider payload must reassemble both blocks, signature intact.
	var blocks []map[string]any
	if err := json.Unmarshal(msg.Provider, &blocks); err != nil || len(blocks) != 2 {
		t.Fatalf("provider = %s (err %v)", msg.Provider, err)
	}
	if blocks[0]["thinking"] != "weigh the options" || blocks[0]["signature"] != "sig-abc" {
		t.Fatalf("thinking block = %+v", blocks[0])
	}
	if blocks[1]["type"] != "redacted_thinking" || blocks[1]["data"] != "opaque-bytes" {
		t.Fatalf("redacted block = %+v", blocks[1])
	}
}

func TestRequestWireFormat(t *testing.T) {
	var captured []byte
	var headers http.Header
	srv := sseServer(t, []string{
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`data: {"type":"message_stop"}`,
	}, &captured, &headers)
	defer srv.Close()

	providerBlocks, _ := json.Marshal([]json.RawMessage{
		json.RawMessage(`{"type":"thinking","thinking":"prior","signature":"sig-1"}`),
	})
	req := agentkit.Request{
		System: "be brief",
		Messages: []agentkit.Message{
			{Role: agentkit.RoleUser, Text: "hi"},
			{Role: agentkit.RoleAssistant, Provider: providerBlocks, ToolCalls: []agentkit.ToolCall{
				{ID: "c1", Name: "search", Args: json.RawMessage(`{"q":"x"}`)},
			}},
			{Role: agentkit.RoleTool, ToolCallID: "c1", ToolName: "search", Text: `{"hits":0}`},
			{Role: agentkit.RoleTool, ToolCallID: "c2", ToolName: "now", Text: `{}`, IsError: true},
		},
		Tools: []agentkit.ToolDef{{Name: "search", Description: "find", Schema: map[string]any{"type": "object"}}},
	}
	// Manual budget: the legacy (pre-4.6) shape.
	c := client(srv.URL)
	c.Model = "claude-sonnet-4-5"
	c.ThinkingBudget = 2048
	if _, err := c.Stream(context.Background(), req, func(agentkit.StreamEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}

	if headers.Get("x-api-key") != "test-key" || headers.Get("anthropic-version") == "" {
		t.Fatalf("auth headers = %+v", headers)
	}
	var wire struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		System    string `json:"system"`
		Stream    bool   `json:"stream"`
		Thinking  *struct {
			Type         string `json:"type"`
			BudgetTokens int    `json:"budget_tokens"`
		} `json:"thinking"`
		Messages []struct {
			Role    string `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(captured, &wire); err != nil {
		t.Fatalf("request body: %v\n%s", err, captured)
	}
	if !wire.Stream || wire.Model != "claude-sonnet-4-5" || wire.System != "be brief" {
		t.Fatalf("wire = %+v", wire)
	}
	if wire.Thinking == nil || wire.Thinking.Type != "enabled" || wire.Thinking.BudgetTokens != 2048 {
		t.Fatalf("thinking = %+v", wire.Thinking)
	}
	if wire.MaxTokens <= wire.Thinking.BudgetTokens {
		t.Fatalf("max_tokens %d must exceed thinking budget %d", wire.MaxTokens, wire.Thinking.BudgetTokens)
	}
	if len(wire.Messages) != 3 {
		t.Fatalf("want user/assistant/user after coalescing, got %d: %+v", len(wire.Messages), wire.Messages)
	}
	// Assistant turn: round-tripped thinking block first, then tool_use.
	asst := wire.Messages[1]
	if asst.Role != "assistant" || asst.Content[0]["type"] != "thinking" || asst.Content[0]["signature"] != "sig-1" {
		t.Fatalf("assistant turn = %+v", asst)
	}
	if asst.Content[1]["type"] != "tool_use" || asst.Content[1]["name"] != "search" {
		t.Fatalf("tool_use lost: %+v", asst.Content)
	}
	// Both tool results coalesce into ONE user turn.
	results := wire.Messages[2]
	if results.Role != "user" || len(results.Content) != 2 {
		t.Fatalf("tool results = %+v", results)
	}
	if results.Content[0]["tool_use_id"] != "c1" || results.Content[1]["is_error"] != true {
		t.Fatalf("tool result blocks = %+v", results.Content)
	}
	if len(wire.Tools) != 1 || wire.Tools[0].Name != "search" || wire.Tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tools = %+v", wire.Tools)
	}
}

func TestModernEffortWireFormat(t *testing.T) {
	// 4.6+ models: output_config.effort, and NO thinking parameter —
	// the manual enabled/budget_tokens shape is a 400 there.
	var captured []byte
	srv := sseServer(t, []string{
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`data: {"type":"message_stop"}`,
	}, &captured, nil)
	defer srv.Close()

	c := client(srv.URL)
	c.Model = "claude-opus-5"
	c.Effort = "high"
	req := agentkit.Request{Messages: []agentkit.Message{{Role: agentkit.RoleUser, Text: "hi"}}}
	if _, err := c.Stream(context.Background(), req, func(agentkit.StreamEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(captured, &wire); err != nil {
		t.Fatalf("request body: %v\n%s", err, captured)
	}
	if _, hasThinking := wire["thinking"]; hasThinking {
		t.Fatalf("modern request must not carry the manual thinking shape: %s", captured)
	}
	if string(wire["output_config"]) != `{"effort":"high"}` {
		t.Fatalf("output_config = %s", wire["output_config"])
	}
}

func TestRefusalIsAnErrorNotSuccess(t *testing.T) {
	// A safety refusal is HTTP 200 + stop_reason "refusal" + empty
	// content. Mapping it to a normal stop would fake a clean finish.
	_, _, err := run(t, []string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"refusal"}}`,
		`data: {"type":"message_stop"}`,
	}, agentkit.Request{})
	if err == nil || !strings.Contains(err.Error(), "refusal") {
		t.Fatalf("err = %v, want refusal error", err)
	}
	if agentkit.IsTransient(err) {
		t.Fatal("a refusal must not be retried")
	}
}

func TestNoStreamEventsIsPermanentError(t *testing.T) {
	// A clean 200 event-stream response with zero events means the
	// endpoint did not actually stream — retrying cannot help.
	_, _, err := run(t, nil, agentkit.Request{})
	if err == nil || agentkit.IsTransient(err) || !strings.Contains(err.Error(), "base URL") {
		t.Fatalf("err = %v, want permanent no-events error", err)
	}
}

func TestLegacyThinkingGeneration(t *testing.T) {
	cases := map[string]bool{
		"claude-sonnet-4-5":          true,
		"claude-haiku-4-5-20251001":  true,
		"claude-opus-4-5":            true,
		"claude-opus-4-1-20250805":   true,
		"claude-opus-4-20250514":     true,
		"claude-3-7-sonnet-20250219": true,
		"claude-3-5-haiku-20241022":  true,
		"claude-sonnet-4-6":          false,
		"claude-opus-4-6":            false,
		"claude-opus-4-7":            false,
		"claude-opus-4-8":            false,
		"claude-opus-5":              false,
		"claude-sonnet-5":            false,
		"claude-fable-5":             false,
		"claude-opus-5-20260601":     false,
		"claude-latest":              false, // unversioned → assume current
	}
	for model, want := range cases {
		if got := LegacyThinking(model); got != want {
			t.Fatalf("LegacyThinking(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestEndpointPathTolerance(t *testing.T) {
	cases := map[string]string{
		"https://api.anthropic.com":     "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com/":    "https://api.anthropic.com/v1/messages",
		"https://gw.example.com/v1":     "https://gw.example.com/v1/messages",
		"https://gw.example.com/v1/":    "https://gw.example.com/v1/messages",
		"https://gw.example.com/api/v1": "https://gw.example.com/api/v1/messages",
	}
	for base, want := range cases {
		c := &Client{BaseURL: base}
		if got := c.endpoint(); got != want {
			t.Fatalf("endpoint(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestStatusClassification(t *testing.T) {
	cases := []struct {
		status    int
		transient bool
	}{
		{429, true}, {500, true}, {529, true}, {401, false}, {400, false}, {404, false},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"x","message":"nope"}}`))
		}))
		_, err := client(srv.URL).Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return nil })
		srv.Close()
		if err == nil {
			t.Fatalf("status %d: want error", tc.status)
		}
		if agentkit.IsTransient(err) != tc.transient {
			t.Fatalf("status %d: transient = %v, want %v (%v)", tc.status, agentkit.IsTransient(err), tc.transient, err)
		}
	}
}

func TestMidStreamTruncationIsTransient(t *testing.T) {
	_, _, err := run(t, []string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"par"}}`,
	}, agentkit.Request{})
	if err == nil || !agentkit.IsTransient(err) {
		t.Fatalf("err = %v, want transient stream-ended error", err)
	}
}

func TestOverloadedStreamErrorIsTransient(t *testing.T) {
	_, _, err := run(t, []string{
		`data: {"type":"error","error":{"type":"overloaded_error","message":"busy"}}`,
	}, agentkit.Request{})
	if err == nil || !agentkit.IsTransient(err) {
		t.Fatalf("err = %v, want transient overloaded error", err)
	}
	_, _, err = run(t, []string{
		`data: {"type":"error","error":{"type":"invalid_request_error","message":"bad thinking block"}}`,
	}, agentkit.Request{})
	if err == nil || agentkit.IsTransient(err) {
		t.Fatalf("err = %v, want permanent request error", err)
	}
}

func TestEmitErrorAbortsStream(t *testing.T) {
	srv := sseServer(t, []string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`,
		`data: {"type":"message_stop"}`,
	}, nil, nil)
	defer srv.Close()
	boom := errors.New("render fail")
	_, err := client(srv.URL).Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want emit error", err)
	}
}

func TestNonStreamResponseIsPermanentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><html>login page</html>"))
	}))
	defer srv.Close()
	_, err := client(srv.URL).Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return nil })
	if err == nil || agentkit.IsTransient(err) || !strings.Contains(err.Error(), "base URL") {
		t.Fatalf("err = %v, want permanent misconfiguration error", err)
	}
}
