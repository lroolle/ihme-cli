package agentkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

// step scripts one model request: events emitted, then the result.
type step struct {
	events []agentkit.StreamEvent
	msg    agentkit.AssistantMessage
	err    error
}

// mock is a scripted Streamer that records every request it serves.
type mock struct {
	steps []step
	calls []agentkit.Request
}

func (m *mock) Stream(ctx context.Context, req agentkit.Request, emit func(agentkit.StreamEvent) error) (agentkit.AssistantMessage, error) {
	m.calls = append(m.calls, req)
	i := len(m.calls) - 1
	if i >= len(m.steps) {
		return agentkit.AssistantMessage{Text: "unscripted", StopReason: agentkit.StopEnd}, nil
	}
	s := m.steps[i]
	for _, ev := range s.events {
		if err := emit(ev); err != nil {
			return agentkit.AssistantMessage{}, err
		}
	}
	return s.msg, s.err
}

func call(id, name, args string) agentkit.ToolCall {
	return agentkit.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
}

func assistantCalling(calls ...agentkit.ToolCall) agentkit.AssistantMessage {
	return agentkit.AssistantMessage{ToolCalls: calls, StopReason: agentkit.StopToolCalls}
}

func done(text string) agentkit.AssistantMessage {
	return agentkit.AssistantMessage{Text: text, StopReason: agentkit.StopEnd}
}

// countingTool records executions and returns a fixed result.
type countingTool struct {
	name  string
	execs int
	fail  error
}

func (t *countingTool) Name() string           { return t.name }
func (t *countingTool) Description() string    { return "test tool" }
func (t *countingTool) Schema() map[string]any { return map[string]any{"type": "object"} }
func (t *countingTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	t.execs++
	if t.fail != nil {
		return nil, t.fail
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func user(text string) []agentkit.Message {
	return []agentkit.Message{{Role: agentkit.RoleUser, Text: text}}
}

func lastToolResult(t *testing.T, transcript []agentkit.Message) agentkit.Message {
	t.Helper()
	for i := len(transcript) - 1; i >= 0; i-- {
		if transcript[i].Role == agentkit.RoleTool {
			return transcript[i]
		}
	}
	t.Fatal("no tool result in transcript")
	return agentkit.Message{}
}

func TestMultiTurnToolLoop(t *testing.T) {
	tool := &countingTool{name: "probe"}
	m := &mock{steps: []step{
		{msg: assistantCalling(call("1", "probe", `{"x":1}`))},
		{msg: assistantCalling(call("2", "probe", `{"x":2}`))},
		{msg: done("finished")},
	}}
	transcript, err := agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: m, Tools: []agentkit.Tool{tool},
	}, user("go"))
	if err != nil {
		t.Fatal(err)
	}
	if tool.execs != 2 {
		t.Fatalf("execs = %d, want 2", tool.execs)
	}
	// The second request must contain the first tool result.
	found := false
	for _, msg := range m.calls[1].Messages {
		if msg.Role == agentkit.RoleTool && strings.Contains(msg.Text, `"ok":true`) {
			found = true
		}
	}
	if !found {
		t.Fatal("tool result not fed back to the model")
	}
	if got := transcript[len(transcript)-1].Text; got != "finished" {
		t.Fatalf("final text = %q", got)
	}
}

// Invariant 1+6: malformed arguments are rejected before the gate,
// never executed, and diagnostics (raw text + parse error) survive.
func TestMalformedArgsRejectedBeforeGate(t *testing.T) {
	tool := &countingTool{name: "probe"}
	gated := 0
	bad := agentkit.ToolCall{
		ID: "1", Name: "probe",
		Args:    json.RawMessage(`{}`),
		RawArgs: `{"x": tru`, ParseErr: "unexpected end of JSON input",
	}
	m := &mock{steps: []step{{msg: assistantCalling(bad)}, {msg: done("ok")}}}
	transcript, err := agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: m, Tools: []agentkit.Tool{tool},
		Gate: func(ctx context.Context, req agentkit.GateRequest) agentkit.GateDecision {
			gated++
			return agentkit.GateDecision{Allowed: true}
		},
	}, user("go"))
	if err != nil {
		t.Fatal(err)
	}
	if gated != 0 {
		t.Fatalf("gate saw malformed call (gated=%d)", gated)
	}
	if tool.execs != 0 {
		t.Fatal("malformed call was executed")
	}
	res := lastToolResult(t, transcript)
	if !res.IsError || !strings.Contains(res.Text, `tru`) || !strings.Contains(res.Text, "unexpected end") {
		t.Fatalf("diagnostics lost: %q", res.Text)
	}
}

// Invariants 2+3: a denial is not executed and its reason reaches
// the model as a tool-result error it can adapt to.
func TestDenialFeedsModel(t *testing.T) {
	tool := &countingTool{name: "reserve"}
	m := &mock{steps: []step{
		{msg: assistantCalling(call("1", "reserve", `{"addr":"a"}`))},
		{msg: done("understood, reporting instead")},
	}}
	_, err := agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: m, Tools: []agentkit.Tool{tool},
		Gate: func(ctx context.Context, req agentkit.GateRequest) agentkit.GateDecision {
			return agentkit.GateDecision{Allowed: false, Reason: "user declined"}
		},
	}, user("go"))
	if err != nil {
		t.Fatal(err)
	}
	if tool.execs != 0 {
		t.Fatal("denied call was executed")
	}
	found := false
	for _, msg := range m.calls[1].Messages {
		if msg.Role == agentkit.RoleTool && msg.IsError && strings.Contains(msg.Text, "user declined") {
			found = true
		}
	}
	if !found {
		t.Fatal("denial reason did not reach the model")
	}
}

// Invariant 4: an identical denied call repeated once terminates.
func TestRepeatedDenialTerminates(t *testing.T) {
	tool := &countingTool{name: "reserve"}
	same := call("1", "reserve", `{"addr":"a"}`)
	m := &mock{steps: []step{
		{msg: assistantCalling(same)},
		{msg: assistantCalling(call("2", "reserve", `{"addr":"a"}`))},
		{msg: done("never reached")},
	}}
	_, err := agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: m, Tools: []agentkit.Tool{tool},
		Gate: func(ctx context.Context, req agentkit.GateRequest) agentkit.GateDecision {
			return agentkit.GateDecision{Allowed: false, Reason: "no"}
		},
	}, user("go"))
	if !errors.Is(err, agentkit.ErrRepeatedDenial) {
		t.Fatalf("err = %v, want ErrRepeatedDenial", err)
	}
	if len(m.calls) != 2 {
		t.Fatalf("model calls = %d, want 2 (terminate on second denial)", len(m.calls))
	}
	// A denied call with DIFFERENT args must not terminate.
	tool2 := &countingTool{name: "reserve"}
	m2 := &mock{steps: []step{
		{msg: assistantCalling(call("1", "reserve", `{"addr":"a"}`))},
		{msg: assistantCalling(call("2", "reserve", `{"addr":"b"}`))},
		{msg: done("ok")},
	}}
	_, err = agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: m2, Tools: []agentkit.Tool{tool2},
		Gate: func(ctx context.Context, req agentkit.GateRequest) agentkit.GateDecision {
			return agentkit.GateDecision{Allowed: false, Reason: "no"}
		},
	}, user("go"))
	if err != nil {
		t.Fatalf("different-args denials must not terminate: %v", err)
	}
}

// Invariant 5: tool calls from a length-truncated response are all
// failed, none executed, and the run continues so the model retries.
func TestTruncatedResponseFailsAllCalls(t *testing.T) {
	tool := &countingTool{name: "probe"}
	truncated := agentkit.AssistantMessage{
		ToolCalls:  []agentkit.ToolCall{call("1", "probe", `{"x":1}`), call("2", "probe", `{"x":2}`)},
		StopReason: agentkit.StopLength,
	}
	m := &mock{steps: []step{{msg: truncated}, {msg: done("recovered")}}}
	transcript, err := agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: m, Tools: []agentkit.Tool{tool},
	}, user("go"))
	if err != nil {
		t.Fatal(err)
	}
	if tool.execs != 0 {
		t.Fatalf("executed %d truncated calls", tool.execs)
	}
	errCount := 0
	for _, msg := range transcript {
		if msg.Role == agentkit.RoleTool && msg.IsError && strings.Contains(msg.Text, "truncated") {
			errCount++
		}
	}
	if errCount != 2 {
		t.Fatalf("truncation errors = %d, want 2", errCount)
	}
}

// Invariant 7: turns, requests, and tool calls are all enforced.
func TestLimits(t *testing.T) {
	looping := func() *mock {
		return &mock{steps: []step{
			{msg: assistantCalling(call("1", "probe", `{}`))},
			{msg: assistantCalling(call("2", "probe", `{}`))},
			{msg: assistantCalling(call("3", "probe", `{}`))},
			{msg: done("x")},
		}}
	}
	cases := []struct {
		name   string
		limits agentkit.Limits
		steps  *mock
		want   string
	}{
		{"turns", agentkit.Limits{MaxTurns: 2, MaxRequests: 99, MaxToolCalls: 99}, looping(), "turns"},
		{"requests", agentkit.Limits{MaxTurns: 99, MaxRequests: 2, MaxToolCalls: 99}, looping(), "requests"},
		{"tool_calls", agentkit.Limits{MaxTurns: 99, MaxRequests: 99, MaxToolCalls: 2}, looping(), "tool_calls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := &countingTool{name: "probe"}
			_, err := agentkit.Run(context.Background(), agentkit.RunConfig{
				Streamer: tc.steps, Tools: []agentkit.Tool{tool}, Limits: tc.limits,
			}, user("go"))
			var lim agentkit.LimitError
			if !errors.As(err, &lim) || lim.Limit != tc.want {
				t.Fatalf("err = %v, want LimitError{%s}", err, tc.want)
			}
		})
	}
}

// Invariant 8: cancellation propagates into tools and stops the run.
func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sawCancel := false
	tool := agentkit.FuncTool{
		ToolName: "probe", Desc: "d", Params: map[string]any{"type": "object"},
		Fn: func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
			cancel()
			sawCancel = ctx.Err() != nil
			return json.RawMessage(`{}`), nil
		},
	}
	m := &mock{steps: []step{
		{msg: assistantCalling(call("1", "probe", `{}`))},
		{msg: done("never")},
	}}
	_, err := agentkit.Run(ctx, agentkit.RunConfig{Streamer: m, Tools: []agentkit.Tool{tool}}, user("go"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !sawCancel {
		t.Fatal("tool did not observe cancellation through its ctx")
	}
}

// Invariant 9a: transient failures before output are retried.
func TestTransientRetriedBeforeOutput(t *testing.T) {
	m := &mock{steps: []step{
		{err: agentkit.Transient{Err: errors.New("429")}},
		{msg: done("recovered")},
	}}
	transcript, err := agentkit.Run(context.Background(), agentkit.RunConfig{Streamer: m}, user("go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (one retry)", len(m.calls))
	}
	if transcript[len(transcript)-1].Text != "recovered" {
		t.Fatal("retry did not recover")
	}
}

// Invariant 9b: transient failures AFTER meaningful output fail the run.
func TestTransientAfterOutputNotRetried(t *testing.T) {
	m := &mock{steps: []step{
		{
			events: []agentkit.StreamEvent{{Type: agentkit.StreamText, Text: "partial"}},
			err:    agentkit.Transient{Err: errors.New("conn reset")},
		},
		{msg: done("must not reach")},
	}}
	_, err := agentkit.Run(context.Background(), agentkit.RunConfig{Streamer: m}, user("go"))
	if err == nil || !agentkit.IsTransient(err) && !strings.Contains(err.Error(), "conn reset") {
		t.Fatalf("err = %v, want the transient error surfaced", err)
	}
	if len(m.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (no retry after output)", len(m.calls))
	}
}

// Invariant 9c: non-transient failures are never retried.
func TestNonTransientNotRetried(t *testing.T) {
	m := &mock{steps: []step{{err: errors.New("401 unauthorized")}}}
	_, err := agentkit.Run(context.Background(), agentkit.RunConfig{Streamer: m}, user("go"))
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
	if len(m.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(m.calls))
	}
}

// Invariant 9d: a failing mutation executes exactly once; its error
// returns to the model and the run continues.
func TestFailingToolNeverRetried(t *testing.T) {
	tool := &countingTool{name: "reserve", fail: errors.New("api 500")}
	m := &mock{steps: []step{
		{msg: assistantCalling(call("1", "reserve", `{}`))},
		{msg: done("reported failure")},
	}}
	transcript, err := agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: m, Tools: []agentkit.Tool{tool},
	}, user("go"))
	if err != nil {
		t.Fatal(err)
	}
	if tool.execs != 1 {
		t.Fatalf("execs = %d, want exactly 1", tool.execs)
	}
	res := lastToolResult(t, transcript)
	if !res.IsError || !strings.Contains(res.Text, "api 500") {
		t.Fatalf("tool error lost: %q", res.Text)
	}
}

// Unknown tools are rejected without execution and the run continues.
func TestUnknownTool(t *testing.T) {
	m := &mock{steps: []step{
		{msg: assistantCalling(call("1", "ghost", `{}`))},
		{msg: done("ok")},
	}}
	transcript, err := agentkit.Run(context.Background(), agentkit.RunConfig{Streamer: m}, user("go"))
	if err != nil {
		t.Fatal(err)
	}
	res := lastToolResult(t, transcript)
	if !res.IsError || !strings.Contains(res.Text, "unknown tool") {
		t.Fatalf("unknown-tool result = %q", res.Text)
	}
}

// Emit errors abort the run: the callback is the renderer's
// backpressure and error path.
func TestEmitErrorAborts(t *testing.T) {
	tool := &countingTool{name: "probe"}
	boom := errors.New("broken pipe")
	m := &mock{steps: []step{
		{msg: assistantCalling(call("1", "probe", `{}`))},
		{msg: done("never")},
	}}
	_, err := agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: m, Tools: []agentkit.Tool{tool},
		OnEvent: func(ev agentkit.Event) error {
			if _, ok := ev.(agentkit.ToolStart); ok {
				return boom
			}
			return nil
		},
	}, user("go"))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want emit error", err)
	}
}

// Lifecycle events arrive in order with model events layered inside
// the turn, and RunEnd reports summed usage.
func TestEventSequenceAndUsage(t *testing.T) {
	tool := &countingTool{name: "probe"}
	m := &mock{steps: []step{
		{
			events: []agentkit.StreamEvent{{Type: agentkit.StreamToolCall, Call: &agentkit.ToolCall{Name: "probe"}}},
			msg: agentkit.AssistantMessage{
				ToolCalls: []agentkit.ToolCall{call("1", "probe", `{}`)}, StopReason: agentkit.StopToolCalls,
				Usage: agentkit.Usage{InputTokens: 10, OutputTokens: 5},
			},
		},
		{
			events: []agentkit.StreamEvent{{Type: agentkit.StreamText, Text: "done"}},
			msg: agentkit.AssistantMessage{
				Text: "done", StopReason: agentkit.StopEnd,
				Usage: agentkit.Usage{InputTokens: 20, OutputTokens: 2},
			},
		},
	}}
	var kinds []string
	var end agentkit.RunEnd
	_, err := agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: m, Tools: []agentkit.Tool{tool},
		OnEvent: func(ev agentkit.Event) error {
			switch e := ev.(type) {
			case agentkit.RunStart:
				kinds = append(kinds, "run_start")
			case agentkit.TurnStart:
				kinds = append(kinds, "turn_start")
			case agentkit.ModelEvent:
				kinds = append(kinds, "model")
			case agentkit.ToolStart:
				kinds = append(kinds, "tool_start")
			case agentkit.ToolEnd:
				kinds = append(kinds, "tool_end")
			case agentkit.TurnEnd:
				kinds = append(kinds, "turn_end")
			case agentkit.RunEnd:
				kinds = append(kinds, "run_end")
				end = e
			}
			return nil
		},
	}, user("go"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run_start", "turn_start", "model", "tool_start", "tool_end", "turn_end", "turn_start", "model", "turn_end", "run_end"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("event order:\n got %v\nwant %v", kinds, want)
	}
	if end.Usage.InputTokens != 30 || end.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %+v, want 30/7", end.Usage)
	}
	if end.Reason != "done" {
		t.Fatalf("reason = %q", end.Reason)
	}
}

// With no gate configured, calls are allowed (policy is opt-in).
func TestNilGateAllows(t *testing.T) {
	tool := &countingTool{name: "probe"}
	m := &mock{steps: []step{
		{msg: assistantCalling(call("1", "probe", `{}`))},
		{msg: done("ok")},
	}}
	_, err := agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: m, Tools: []agentkit.Tool{tool},
	}, user("go"))
	if err != nil {
		t.Fatal(err)
	}
	if tool.execs != 1 {
		t.Fatalf("execs = %d, want 1", tool.execs)
	}
}
