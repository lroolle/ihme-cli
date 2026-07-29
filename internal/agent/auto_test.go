package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"github.com/lroolle/ihme-cli/pkg/agentkit/ai/anthropic"
	"github.com/lroolle/ihme-cli/pkg/agentkit/ai/openai"
)

// fakeStreamer scripts one Stream result per API kind.
type fakeStreamer struct {
	events []agentkit.StreamEvent
	msg    agentkit.AssistantMessage
	err    error
	calls  int
}

func (f *fakeStreamer) Stream(ctx context.Context, req agentkit.Request, emit func(agentkit.StreamEvent) error) (agentkit.AssistantMessage, error) {
	f.calls++
	for _, ev := range f.events {
		if err := emit(ev); err != nil {
			return agentkit.AssistantMessage{}, err
		}
	}
	return f.msg, f.err
}

// the gateway 400 that started all this
var needsResponses = &openai.APIError{
	Status: 400,
	Body:   `{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-5.6-sol in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'."}}`,
}

func autoWith(api string, locked bool, byAPI map[string]*fakeStreamer, persisted *[]string) *autoStreamer {
	return &autoStreamer{
		api:    api,
		locked: locked,
		make:   func(api string) agentkit.Streamer { return byAPI[api] },
		persist: func(api string) {
			if persisted != nil {
				*persisted = append(*persisted, api)
			}
		},
	}
}

func TestAutoFlipsCompletionsToResponses(t *testing.T) {
	completions := &fakeStreamer{err: needsResponses}
	responses := &fakeStreamer{msg: agentkit.AssistantMessage{Text: "ok", StopReason: agentkit.StopEnd}}
	var persisted []string
	a := autoWith("completions", false, map[string]*fakeStreamer{
		"completions": completions, "responses": responses,
	}, &persisted)

	msg, err := a.Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return nil })
	if err != nil || msg.Text != "ok" {
		t.Fatalf("msg=%+v err=%v", msg, err)
	}
	if completions.calls != 1 || responses.calls != 1 {
		t.Fatalf("calls: completions=%d responses=%d", completions.calls, responses.calls)
	}
	if len(persisted) != 1 || persisted[0] != "responses" {
		t.Fatalf("persisted = %v", persisted)
	}
	// Subsequent requests go straight to responses.
	if _, err := a.Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if completions.calls != 1 || responses.calls != 2 {
		t.Fatalf("second request went to the wrong API: completions=%d responses=%d", completions.calls, responses.calls)
	}
}

func TestAutoFlipsResponsesTo404Completions(t *testing.T) {
	responses := &fakeStreamer{err: &openai.APIError{Status: 404, Body: "not found"}}
	completions := &fakeStreamer{msg: agentkit.AssistantMessage{Text: "ok"}}
	var persisted []string
	a := autoWith("responses", false, map[string]*fakeStreamer{
		"completions": completions, "responses": responses,
	}, &persisted)
	if _, err := a.Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0] != "completions" {
		t.Fatalf("persisted = %v", persisted)
	}
}

func TestPinnedConfigNeverFlips(t *testing.T) {
	completions := &fakeStreamer{err: needsResponses}
	responses := &fakeStreamer{msg: agentkit.AssistantMessage{Text: "never"}}
	a := autoWith("completions", true, map[string]*fakeStreamer{
		"completions": completions, "responses": responses,
	}, nil)
	_, err := a.Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return nil })
	if err == nil || responses.calls != 0 {
		t.Fatalf("pinned config flipped: err=%v responses.calls=%d", err, responses.calls)
	}
}

func TestNoFlipAfterMeaningfulOutput(t *testing.T) {
	completions := &fakeStreamer{
		events: []agentkit.StreamEvent{{Type: agentkit.StreamText, Text: "partial"}},
		err:    needsResponses, // pathological, but output already reached the user
	}
	responses := &fakeStreamer{msg: agentkit.AssistantMessage{Text: "never"}}
	a := autoWith("completions", false, map[string]*fakeStreamer{
		"completions": completions, "responses": responses,
	}, nil)
	_, err := a.Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return nil })
	if err == nil || responses.calls != 0 {
		t.Fatalf("flipped after output: err=%v responses.calls=%d", err, responses.calls)
	}
}

func TestNonMisrouteErrorsPassThrough(t *testing.T) {
	completions := &fakeStreamer{err: errors.New("401 unauthorized")}
	responses := &fakeStreamer{}
	a := autoWith("completions", false, map[string]*fakeStreamer{
		"completions": completions, "responses": responses,
	}, nil)
	_, err := a.Stream(context.Background(), agentkit.Request{}, func(agentkit.StreamEvent) error { return nil })
	if err == nil || responses.calls != 0 {
		t.Fatalf("flipped on a non-misroute error: %v", err)
	}
}

func TestMisrouteUnwrapsTransient(t *testing.T) {
	// APIError wrapped in Transient (5xx path) must still classify.
	if _, ok := misroute("completions", agentkit.Transient{Err: needsResponses}); !ok {
		t.Fatal("misroute must unwrap")
	}
}

func TestGuessAPI(t *testing.T) {
	cases := map[string]string{
		"gpt-5.6-sol":       "responses",
		"gpt-5-mini":        "responses",
		"o3-pro":            "responses",
		"o4-mini":           "responses",
		"codex-mini":        "responses",
		"claude-opus-5":     "anthropic",
		"claude-sonnet-4-5": "anthropic",
		"gpt-4o-mini":       "completions",
		"deepseek-chat":     "completions",
		"gemini-2.5-flash":  "completions",
		"llama3.3":          "completions",
	}
	for model, want := range cases {
		if got := guessAPI(model, "https://gw.example.com/v1"); got != want {
			t.Fatalf("guessAPI(%q) = %q, want %q", model, got, want)
		}
	}
	// Anthropic's own host wins regardless of model naming.
	if got := guessAPI("anything", "https://api.anthropic.com"); got != "anthropic" {
		t.Fatalf("anthropic host guess = %q", got)
	}
}

func TestMisrouteAnthropicFallsBackToCompletions(t *testing.T) {
	// A claude model pointed at an OpenAI-protocol gateway: the
	// gateway has no /v1/messages, so 404 must flip to completions.
	noMessages := &anthropic.APIError{Status: 404, Body: "404 page not found"}
	next, ok := misroute("anthropic", noMessages)
	if !ok || next != "completions" {
		t.Fatalf("misroute = %q, %v", next, ok)
	}
	// A permanent model error on the native API must NOT flip.
	if _, ok := misroute("anthropic", &anthropic.APIError{Status: 400, Body: `{"error":{"type":"invalid_request_error","message":"max_tokens too large"}}`}); ok {
		t.Fatal("400 without path signal must not flip")
	}
}

func TestThinkingBudgetMapsSharedEffortVocabulary(t *testing.T) {
	cases := map[string]int{
		"minimal": 1024, "low": 2048, "medium": 8192, "high": 16384,
		"": 0, "ultra": 0,
	}
	for effort, want := range cases {
		if got := thinkingBudget(effort); got != want {
			t.Fatalf("thinkingBudget(%q) = %d, want %d", effort, got, want)
		}
	}
}

func TestAnthropicEffortMapsSharedVocabulary(t *testing.T) {
	cases := map[string]string{
		"minimal": "low", // Anthropic's floor
		"low":     "low",
		"medium":  "medium",
		"high":    "high",
		"xhigh":   "xhigh",
		"max":     "max",
		"":        "",
		"ultra":   "", // unknown → omit, model default applies
	}
	for effort, want := range cases {
		if got := anthropicEffort(effort); got != want {
			t.Fatalf("anthropicEffort(%q) = %q, want %q", effort, got, want)
		}
	}
}
