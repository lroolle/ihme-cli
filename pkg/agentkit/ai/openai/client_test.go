package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

// sseServer serves scripted SSE lines for one request and captures
// the request body.
func sseServer(t *testing.T, lines []string, capture *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			*capture = body
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range lines {
			_, _ = w.Write([]byte(line + "\n\n"))
		}
	}))
}

func client(url string) *Client {
	return &Client{BaseURL: url, APIKey: "test-key", Model: "test-model"}
}

func run(t *testing.T, lines []string, req agentkit.Request) (agentkit.AssistantMessage, []agentkit.StreamEvent, error) {
	t.Helper()
	srv := sseServer(t, lines, nil)
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
		`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"choices":[{"delta":{"content":"lo"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":4}}`,
		`data: [DONE]`,
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

func TestToolCallAssemblyAcrossFragments(t *testing.T) {
	msg, events, err := run(t, []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"search","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"github\"}"}},{"index":1,"id":"c2","function":{"name":"now","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
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
	if msg.ToolCalls[1].Name != "now" || string(msg.ToolCalls[1].Args) != "{}" {
		t.Fatalf("call1 = %+v", msg.ToolCalls[1])
	}
	// Each call announced once, by name, as soon as it appeared.
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

func TestMalformedArgsPreserved(t *testing.T) {
	msg, _, err := run(t, []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"reserve","arguments":"{\"addr\": tru"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
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

func TestLengthFinishMapsToStopLength(t *testing.T) {
	msg, _, err := run(t, []string{
		`data: {"choices":[{"delta":{"content":"partial"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}`,
		`data: [DONE]`,
	}, agentkit.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != agentkit.StopLength {
		t.Fatalf("stop = %q", msg.StopReason)
	}
}

func TestStatusClassification(t *testing.T) {
	cases := []struct {
		status    int
		transient bool
	}{
		{429, true}, {500, true}, {503, true}, {401, false}, {400, false},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
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
	// Server closes without finish_reason or [DONE].
	_, _, err := run(t, []string{
		`data: {"choices":[{"delta":{"content":"par"}}]}`,
	}, agentkit.Request{})
	if err == nil || !agentkit.IsTransient(err) {
		t.Fatalf("err = %v, want transient stream-ended error", err)
	}
}

func TestEmitErrorAbortsStream(t *testing.T) {
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"content":"x"}}]}`,
		`data: [DONE]`,
	}, nil)
	defer srv.Close()
	boom := errors.New("render fail")
	_, err := client(srv.URL).Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want emit error", err)
	}
}

func TestRequestWireFormat(t *testing.T) {
	var captured []byte
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}, &captured)
	defer srv.Close()

	req := agentkit.Request{
		System: "be brief",
		Messages: []agentkit.Message{
			{Role: agentkit.RoleUser, Text: "hi"},
			{Role: agentkit.RoleAssistant, ToolCalls: []agentkit.ToolCall{
				{ID: "c1", Name: "search", Args: json.RawMessage(`{"q":"x"}`)},
			}},
			{Role: agentkit.RoleTool, ToolCallID: "c1", ToolName: "search", Text: `{"hits":0}`},
		},
		Tools: []agentkit.ToolDef{{Name: "search", Description: "find", Schema: map[string]any{"type": "object"}}},
	}
	if _, err := client(srv.URL).Stream(context.Background(), req, func(agentkit.StreamEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}

	var wire struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []struct {
			Type string `json:"type"`
		} `json:"tools"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	if err := json.Unmarshal(captured, &wire); err != nil {
		t.Fatalf("request body: %v\n%s", err, captured)
	}
	if !wire.Stream || !wire.StreamOptions.IncludeUsage || wire.Model != "test-model" {
		t.Fatalf("wire = %+v", wire)
	}
	if wire.Messages[0].Role != "system" || wire.Messages[0].Content != "be brief" {
		t.Fatalf("system message: %+v", wire.Messages[0])
	}
	if wire.Messages[2].ToolCalls[0].Function.Name != "search" {
		t.Fatalf("assistant tool call lost: %+v", wire.Messages[2])
	}
	if wire.Messages[3].Role != "tool" || wire.Messages[3].ToolCallID != "c1" {
		t.Fatalf("tool result mapping: %+v", wire.Messages[3])
	}
	if len(wire.Tools) != 1 || wire.Tools[0].Type != "function" {
		t.Fatalf("tools = %+v", wire.Tools)
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
