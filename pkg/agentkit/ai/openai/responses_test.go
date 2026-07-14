package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

func respClient(url string) *ResponsesClient {
	return &ResponsesClient{BaseURL: url, APIKey: "test-key", Model: "test-model"}
}

func runResp(t *testing.T, lines []string, req agentkit.Request) (agentkit.AssistantMessage, []agentkit.StreamEvent, error) {
	t.Helper()
	srv := sseServer(t, lines, nil)
	defer srv.Close()
	var events []agentkit.StreamEvent
	msg, err := respClient(srv.URL).Stream(context.Background(), req, func(ev agentkit.StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	return msg, events, err
}

func TestResponsesTextStreaming(t *testing.T) {
	msg, events, err := runResp(t, []string{
		`data: {"type":"response.output_text.delta","delta":"Hel"}`,
		`data: {"type":"response.output_text.delta","delta":"lo"}`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":9,"output_tokens":3}}}`,
		`data: [DONE]`,
	}, agentkit.Request{Messages: []agentkit.Message{{Role: agentkit.RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "Hello" || msg.StopReason != agentkit.StopEnd {
		t.Fatalf("msg = %+v", msg)
	}
	if msg.Usage.InputTokens != 9 || msg.Usage.OutputTokens != 3 {
		t.Fatalf("usage = %+v", msg.Usage)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
}

func TestResponsesFunctionCall(t *testing.T) {
	msg, events, err := runResp(t, []string{
		`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_1","name":"search","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","delta":"{\"q\":"}`,
		`data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_1","name":"search","arguments":"{\"q\":\"github\"}"}}`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":5,"output_tokens":2}}}`,
		`data: [DONE]`,
	}, agentkit.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != agentkit.StopToolCalls || len(msg.ToolCalls) != 1 {
		t.Fatalf("msg = %+v", msg)
	}
	call := msg.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "search" || string(call.Args) != `{"q":"github"}` {
		t.Fatalf("call = %+v", call)
	}
	announced := 0
	for _, ev := range events {
		if ev.Type == agentkit.StreamToolCall && ev.Call.Name == "search" {
			announced++
		}
	}
	if announced != 1 {
		t.Fatalf("announced = %d", announced)
	}
}

func TestResponsesTruncationMapsToStopLength(t *testing.T) {
	msg, _, err := runResp(t, []string{
		`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"c","name":"reserve","arguments":""}}`,
		`data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"c","name":"reserve","arguments":"{\"addr"}}`,
		`data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`,
		`data: [DONE]`,
	}, agentkit.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != agentkit.StopLength {
		t.Fatalf("stop = %q, want length (kernel must fail these calls)", msg.StopReason)
	}
	// Truncated args also surface as a parse diagnostic.
	if msg.ToolCalls[0].ParseErr == "" {
		t.Fatalf("truncated args should carry ParseErr: %+v", msg.ToolCalls[0])
	}
}

func TestResponsesStreamErrorEvent(t *testing.T) {
	_, _, err := runResp(t, []string{
		`data: {"type":"error","message":"boom"}`,
	}, agentkit.Request{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestResponsesEndsWithoutCompletionIsTransient(t *testing.T) {
	_, _, err := runResp(t, []string{
		`data: {"type":"response.output_text.delta","delta":"par"}`,
	}, agentkit.Request{})
	if err == nil || !agentkit.IsTransient(err) {
		t.Fatalf("err = %v, want transient", err)
	}
}

func TestResponsesRequestWireFormat(t *testing.T) {
	var captured []byte
	srv := sseServer(t, []string{
		`data: {"type":"response.completed","response":{"status":"completed"}}`,
		`data: [DONE]`,
	}, &captured)
	defer srv.Close()

	c := respClient(srv.URL)
	c.Effort = "low"
	req := agentkit.Request{
		System: "rules",
		Messages: []agentkit.Message{
			{Role: agentkit.RoleUser, Text: "hi"},
			{Role: agentkit.RoleAssistant, Text: "thinking", ToolCalls: []agentkit.ToolCall{
				{ID: "call_1", Name: "search", Args: json.RawMessage(`{"q":"x"}`)},
			}},
			{Role: agentkit.RoleTool, ToolCallID: "call_1", Text: `{"hits":0}`},
		},
		Tools: []agentkit.ToolDef{{Name: "search", Description: "find", Schema: map[string]any{"type": "object"}}},
	}
	if _, err := c.Stream(context.Background(), req, func(agentkit.StreamEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}

	var wire struct {
		Instructions string `json:"instructions"`
		Store        bool   `json:"store"`
		Reasoning    *struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		Input []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			CallID  string `json:"call_id"`
			Output  string `json:"output"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
		Tools []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(captured, &wire); err != nil {
		t.Fatalf("request body: %v\n%s", err, captured)
	}
	if wire.Instructions != "rules" || wire.Store {
		t.Fatalf("instructions/store: %+v", wire)
	}
	if wire.Reasoning == nil || wire.Reasoning.Effort != "low" {
		t.Fatalf("reasoning = %+v", wire.Reasoning)
	}
	// user msg, assistant text, function_call, function_call_output
	if len(wire.Input) != 4 {
		t.Fatalf("input items = %d: %+v", len(wire.Input), wire.Input)
	}
	if wire.Input[0].Content[0].Type != "input_text" {
		t.Fatalf("user content: %+v", wire.Input[0])
	}
	if wire.Input[1].Content[0].Type != "output_text" {
		t.Fatalf("assistant content: %+v", wire.Input[1])
	}
	if wire.Input[2].Type != "function_call" || wire.Input[2].CallID != "call_1" {
		t.Fatalf("function_call item: %+v", wire.Input[2])
	}
	if wire.Input[3].Type != "function_call_output" || wire.Input[3].Output != `{"hits":0}` {
		t.Fatalf("function_call_output item: %+v", wire.Input[3])
	}
	// Responses tools are FLAT — name at top level, not under "function".
	if wire.Tools[0].Type != "function" || wire.Tools[0].Name != "search" {
		t.Fatalf("tools = %+v", wire.Tools)
	}
}

func TestResponsesNoEffortOmitsReasoning(t *testing.T) {
	var captured []byte
	srv := sseServer(t, []string{
		`data: {"type":"response.completed","response":{"status":"completed"}}`,
		`data: [DONE]`,
	}, &captured)
	defer srv.Close()
	if _, err := respClient(srv.URL).Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(captured), "reasoning") {
		t.Fatalf("reasoning must be omitted when Effort is empty: %s", captured)
	}
}

func TestResponsesStatusClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer srv.Close()
	_, err := respClient(srv.URL).Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return nil })
	if err == nil || !agentkit.IsTransient(err) {
		t.Fatalf("err = %v, want transient 429", err)
	}
}

func TestResponsesThinkingSummaries(t *testing.T) {
	_, events, err := runResp(t, []string{
		`data: {"type":"response.reasoning_summary_text.delta","delta":"weighing the two candidates"}`,
		`data: {"type":"response.output_text.delta","delta":"done"}`,
		`data: {"type":"response.completed","response":{"status":"completed"}}`,
		`data: [DONE]`,
	}, agentkit.Request{})
	if err != nil {
		t.Fatal(err)
	}
	thinking := 0
	for _, ev := range events {
		if ev.Type == agentkit.StreamThinking && strings.Contains(ev.Text, "weighing") {
			thinking++
		}
	}
	if thinking != 1 {
		t.Fatalf("thinking events = %d, want 1", thinking)
	}
}
