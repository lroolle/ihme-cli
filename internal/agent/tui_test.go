package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

func key(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func TestTUIPromptKeepsNextTurnDraftSeparate(t *testing.T) {
	m := newTUIModel(context.Background(), nil, "person@example.com", GrantAsk)
	m.phase = phaseRunning
	m.input.SetValue("draft for my next request")
	replies := make(chan promptReply, 1)

	m.Update(promptRequestMsg{
		prompt: userPrompt{Kind: promptQuestion, Title: "Use the exact label?"},
		reply:  replies,
	})
	if m.phase != phaseQuestion || m.input.Value() != "" {
		t.Fatalf("question state = %v input = %q", m.phase, m.input.Value())
	}

	m.input.SetValue("yes, exact")
	m.handleKey(key(tea.KeyEnter))
	reply := <-replies
	if reply.err != nil || reply.answer != "yes, exact" {
		t.Fatalf("reply = %+v", reply)
	}
	if m.phase != phaseRunning || m.input.Value() != "draft for my next request" {
		t.Fatalf("draft was not restored: phase=%v input=%q", m.phase, m.input.Value())
	}
}

func TestTUIConsentIsAChoiceAndDefaultsToDeny(t *testing.T) {
	m := newTUIModel(context.Background(), nil, "person@example.com", GrantAsk)
	m.phase = phaseRunning
	replies := make(chan promptReply, 1)

	m.Update(promptRequestMsg{
		prompt: userPrompt{
			Kind:    promptConsent,
			Title:   "Create this Hide My Email address?",
			Subject: "calm.river@icloud.com",
			Facts:   [][2]string{{"label", "grok"}, {"note", "grok signup 2026-07"}},
			Why:     "a calm river bend — kept for the image",
			Passed:  [][2]string{{"turbo3_placard", "embedded digit"}, {"pale_slate", "no image"}},
		},
		reply: replies,
	})
	if m.phase != phaseConsent || m.choice != 1 {
		t.Fatalf("phase=%v choice=%d", m.phase, m.choice)
	}
	// Collapse soft line wraps so long phrases match across breaks.
	view := strings.Join(strings.Fields(m.View().Content), " ")
	for _, want := range []string{
		"Allow once", "Deny", "Always this run",
		"calm.river@icloud.com", // the subject is on the card
		"grok signup 2026-07",   // what gets written is on the card
		"calm river bend",       // the verdict is on the card
		"turbo3_placard",        // and the candidates it beat,
		"no image",              // each with its failure
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("consent view missing %q: %q", want, view)
		}
	}

	m.handleKey(key(tea.KeyEnter))
	reply := <-replies
	if reply.answer != "n" || reply.err != nil {
		t.Fatalf("default consent reply = %+v", reply)
	}
	if len(m.steps) != 1 || !strings.Contains(m.steps[0].text, "Denied") {
		t.Fatalf("steps = %+v", m.steps)
	}
}

func TestTUIConsentTypedReplyRedirects(t *testing.T) {
	m := newTUIModel(context.Background(), nil, "person@example.com", GrantAsk)
	m.phase = phaseRunning
	replies := make(chan promptReply, 1)
	m.Update(promptRequestMsg{
		prompt: userPrompt{Kind: promptConsent, Title: "Create this Hide My Email address?", Subject: "turbo3_placard@icloud.com"},
		reply:  replies,
	})

	// Letters go to the reply input — no instant y/n/a firing.
	if _, handled := m.handleKey(key('n')); handled {
		t.Fatal("letter keys must not decide consent instantly")
	}
	select {
	case r := <-replies:
		t.Fatalf("consent decided by a keystroke: %+v", r)
	default:
	}

	m.input.SetValue("use the calm river one instead")
	m.handleKey(key(tea.KeyEnter))
	reply := <-replies
	if reply.err != nil || reply.answer != "use the calm river one instead" {
		t.Fatalf("reply = %+v", reply)
	}
	if m.phase != phaseRunning {
		t.Fatalf("phase after reply = %v", m.phase)
	}
	found := false
	for _, step := range m.steps {
		found = found || strings.Contains(step.text, "Redirected · use the calm river one instead")
	}
	if !found {
		t.Fatalf("redirect not recorded in steps: %+v", m.steps)
	}
}

func TestTUIConsentTypedShortcutsStillMeanButtons(t *testing.T) {
	m := newTUIModel(context.Background(), nil, "person@example.com", GrantAsk)
	m.phase = phaseRunning
	replies := make(chan promptReply, 1)
	m.Update(promptRequestMsg{
		prompt: userPrompt{Kind: promptConsent, Title: "Create?"},
		reply:  replies,
	})
	m.input.SetValue("y")
	m.handleKey(key(tea.KeyEnter))
	reply := <-replies
	if reply.answer != "y" {
		t.Fatalf("typed y should answer y, got %+v", reply)
	}
	if len(m.steps) != 1 || !strings.Contains(m.steps[0].text, "Allowed once") {
		t.Fatalf("typed y must record the button outcome: %+v", m.steps)
	}
}

func TestTUIThinkingIsLiveStatusNotTranscript(t *testing.T) {
	m := newTUIModel(context.Background(), nil, "person@example.com", GrantAsk)
	m.phase = phaseRunning
	m.currentUser = "new address for grok"
	m.handleAgentEvent(agentkit.ModelEvent{
		Turn: 1,
		Stream: agentkit.StreamEvent{
			Type: agentkit.StreamThinking,
			Text: "I need to reason through canonical labels in detail",
		},
	})
	// While the model thinks, the status line shows the reasoning tail.
	if !strings.Contains(m.renderTurn(), "canonical labels") {
		t.Fatalf("live reasoning tail missing from status: %q", m.activity)
	}

	m.handleAgentEvent(agentkit.ToolEnd{
		Call:   agentkit.ToolCall{Name: "search_addresses", Args: json.RawMessage(`{"query":"grok"}`)},
		Result: json.RawMessage(`{"addresses":[],"count":0}`),
	})
	m.activity = "" // what finishTurn does before printing the block

	view := m.renderTurn()
	if strings.Contains(view, "canonical labels") || strings.Contains(view, `{"addresses"`) {
		t.Fatalf("internal stream leaked into transcript: %q", view)
	}
	if !strings.Contains(view, `No addresses found for "grok"`) {
		t.Fatalf("human tool summary missing: %q", view)
	}
}

func TestThinkingActivityShowsTruncatedTail(t *testing.T) {
	long := "weighing candidates " + strings.Repeat("x", 80)
	got := thinkingActivity("first line\n\n" + long)
	runes := []rune(long)
	want := "Thinking · …" + string(runes[len(runes)-64:])
	if got != want {
		t.Fatalf("thinkingActivity = %q, want %q", got, want)
	}
	if short := thinkingActivity("short thought"); short != "Thinking · short thought" {
		t.Fatalf("short reasoning mangled: %q", short)
	}
	if thinkingActivity("\n \n") != "Thinking" {
		t.Fatalf("blank reasoning should fall back to plain Thinking")
	}
}

func TestRenderInlineStylesMarkdownAndKeepsUnderscores(t *testing.T) {
	styles := newTUIStyles(true)
	got := renderInline("pick **calm.river** over *dull_pick* via `taste`", styles)
	for _, marker := range []string{"**", "`"} {
		if strings.Contains(got, marker) {
			t.Fatalf("marker %q survived rendering: %q", marker, got)
		}
	}
	for _, want := range []string{"calm.river", "dull_pick", "taste"} {
		if !strings.Contains(got, want) {
			t.Fatalf("content %q lost in rendering: %q", want, got)
		}
	}
}

func TestReserveStepSpeaksTheRationale(t *testing.T) {
	step, ok := toolStep(agentkit.ToolEnd{
		Call: agentkit.ToolCall{
			Name: "reserve_address",
			Args: json.RawMessage(`{"address":"calm.river@icloud.com","label":"grok","rationale":"a quiet river bend; rejected digit.soup (leading digits) and blank.slate (no image)"}`),
		},
		Result: json.RawMessage(`{"status":"reserved","address":{"hme":"calm.river@icloud.com","label":"grok"},"copiedToClipboard":true}`),
	})
	if !ok {
		t.Fatal("reserve result was not rendered")
	}
	if !strings.Contains(step.text, "calm.river@icloud.com") || !strings.Contains(step.text, "copied") {
		t.Fatalf("step text = %q", step.text)
	}
	if !strings.Contains(step.detail, "quiet river bend") || !strings.Contains(step.detail, "leading digits") {
		t.Fatalf("rationale missing from step detail: %q", step.detail)
	}
}

func TestToolStepCollapsesCandidateRounds(t *testing.T) {
	m := newTUIModel(context.Background(), nil, "person@example.com", GrantAsk)
	for round := 1; round <= 3; round++ {
		result, _ := json.Marshal(map[string]any{
			"candidates": []string{"one@icloud.com", "two@icloud.com", "three@icloud.com"},
			"round":      round, "roundsLeft": 3 - round,
		})
		step, ok := toolStep(agentkit.ToolEnd{
			Call:   agentkit.ToolCall{Name: "generate_candidates"},
			Result: result,
		})
		if !ok {
			t.Fatal("generate result was not rendered")
		}
		m.upsertStep(step)
	}
	if len(m.steps) != 1 || m.steps[0].text != "Reviewed 3 rounds of address ideas" {
		t.Fatalf("steps = %+v", m.steps)
	}
}

func TestSafeTextDropsTerminalControls(t *testing.T) {
	got := safeText("safe\x1b]52;c;secret\a still here\nnext")
	if strings.ContainsAny(got, "\x1b\a") || !strings.Contains(got, "safe]52;c;secret still here\nnext") {
		t.Fatalf("safeText = %q", got)
	}
}

func TestReserveEmitsAMemoryStatusStep(t *testing.T) {
	m := newTUIModel(context.Background(), nil, "person@example.com", GrantAsk)
	m.handleAgentEvent(agentkit.ToolEnd{
		Call:   agentkit.ToolCall{Name: "reserve_address", Args: json.RawMessage(`{"rationale":"a quiet river bend that holds the eye"}`)},
		Result: json.RawMessage(`{"status":"reserved","address":{"hme":"calm.river@icloud.com","label":"undetectable"},"memory":{"status":"created","topic":"undetectable"}}`),
	})
	var texts []string
	for _, s := range m.steps {
		texts = append(texts, s.text)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, `Memory created for "undetectable"`) {
		t.Fatalf("memory operation invisible in steps: %q", joined)
	}
	if !strings.Contains(joined, "Reserved calm.river@icloud.com") {
		t.Fatalf("reserve step lost: %q", joined)
	}

	// A failed write must not read as success.
	m.steps = nil
	m.handleAgentEvent(agentkit.ToolEnd{
		Call:   agentkit.ToolCall{Name: "reserve_address"},
		Result: json.RawMessage(`{"status":"reserved","address":{"hme":"x@icloud.com"},"memory":{"status":"failed","topic":"undetectable"}}`),
	})
	found := false
	for _, s := range m.steps {
		if strings.Contains(s.text, "Memory write failed") && s.level == stepWarn {
			found = true
		}
	}
	if !found {
		t.Fatalf("failed memory write not surfaced truthfully: %+v", m.steps)
	}
}

func TestRecallStepDistinguishesReuseFromMiss(t *testing.T) {
	hit, ok := toolStep(agentkit.ToolEnd{
		Call:   agentkit.ToolCall{Name: "recall_memory", Args: json.RawMessage(`{"query":"undetectable"}`)},
		Result: json.RawMessage(`{"hits":[{}],"count":1}`),
	})
	if !ok || !strings.Contains(hit.text, `Reused memory for "undetectable"`) || hit.level != stepOK {
		t.Fatalf("recall hit step = %+v", hit)
	}
	miss, ok := toolStep(agentkit.ToolEnd{
		Call:   agentkit.ToolCall{Name: "recall_memory", Args: json.RawMessage(`{"query":"netflix"}`)},
		Result: json.RawMessage(`{"hits":[],"count":0}`),
	})
	if !ok || !strings.Contains(miss.text, `No memory of "netflix" yet`) || miss.level != stepInfo {
		t.Fatalf("recall miss step = %+v", miss)
	}
}

func TestRememberStepStatesTheOperation(t *testing.T) {
	step, ok := toolStep(agentkit.ToolEnd{
		Call:   agentkit.ToolCall{Name: "remember", Args: json.RawMessage(`{"topic":"preferences"}`)},
		Result: json.RawMessage(`{"remembered":"preferences","status":"created","alwaysLoaded":false}`),
	})
	if !ok || step.text != `Memory created for "preferences"` {
		t.Fatalf("remember step = %+v", step)
	}
	step, ok = toolStep(agentkit.ToolEnd{
		Call:   agentkit.ToolCall{Name: "remember", Args: json.RawMessage(`{"topic":"flashcards"}`)},
		Result: json.RawMessage(`{"remembered":"flashcards","status":"updated","alwaysLoaded":true}`),
	})
	if !ok || step.text != `Memory updated for "flashcards" (loaded every run)` {
		t.Fatalf("remember flashcards step = %+v", step)
	}
}

func TestSessionHeaderReportsEffectiveModelAndEffort(t *testing.T) {
	s := &session{model: "gpt-5.6", effort: "high", api: "responses"}
	if got := s.header(); got != "Model: gpt-5.6\nThinking effort: high" {
		t.Fatalf("header = %q", got)
	}
	// Empty effort means the endpoint default applies — say that, do
	// not invent a value.
	s = &session{model: "gpt-5.6", api: "responses"}
	if got := s.header(); got != "Model: gpt-5.6\nThinking effort: default" {
		t.Fatalf("header = %q", got)
	}
	// Chat completions carries reasoning_effort too, but only thinking
	// models act on it: name the parameter that went on the wire rather
	// than promising the model honored it.
	s = &session{model: "deepseek-v4-flash", effort: "high", api: "completions"}
	if got := s.header(); got != "Model: deepseek-v4-flash\nThinking effort: high (sent as reasoning_effort)" {
		t.Fatalf("header = %q", got)
	}
	// Nothing configured → nothing sent; the model's own default rules.
	s = &session{model: "gemini-2.5-pro", api: "completions"}
	if got := s.header(); got != "Model: gemini-2.5-pro\nThinking effort: default" {
		t.Fatalf("header = %q", got)
	}
	// Modern Claude (4.6+): effort is the applied control; report the
	// wire value, including the minimal→low fold.
	s = &session{model: "claude-opus-5", effort: "high", api: "anthropic"}
	if got := s.header(); got != "Model: claude-opus-5\nThinking effort: high" {
		t.Fatalf("header = %q", got)
	}
	s = &session{model: "claude-opus-5", effort: "minimal", api: "anthropic"}
	if got := s.header(); got != "Model: claude-opus-5\nThinking effort: low" {
		t.Fatalf("header = %q", got)
	}
	// No effort → the parameter is omitted and the API default rules.
	s = &session{model: "claude-opus-5", api: "anthropic"}
	if got := s.header(); got != "Model: claude-opus-5\nThinking effort: high (default)" {
		t.Fatalf("header = %q", got)
	}
	// An unmapped effort applies nothing — never echo it as applied.
	s = &session{model: "claude-opus-5", effort: "ultra", api: "anthropic"}
	if got := s.header(); got != "Model: claude-opus-5\nThinking effort: ultra (unrecognized — model default applies)" {
		t.Fatalf("header = %q", got)
	}
	// Legacy Claude (pre-4.6): manual thinking; empty means none sent.
	s = &session{model: "claude-sonnet-4-5", effort: "high", api: "anthropic"}
	if got := s.header(); got != "Model: claude-sonnet-4-5\nThinking effort: high" {
		t.Fatalf("header = %q", got)
	}
	s = &session{model: "claude-sonnet-4-5", api: "anthropic"}
	if got := s.header(); got != "Model: claude-sonnet-4-5\nThinking effort: off" {
		t.Fatalf("header = %q", got)
	}
}

func TestWelcomeShowsModelHeader(t *testing.T) {
	m := newTUIModel(context.Background(), nil, "person@example.com", GrantAsk)
	m.session = &session{st: newRunState(""), model: "gpt-5.6", effort: "high", api: "responses"}
	view := strings.Join(strings.Fields(m.renderWelcome(80)), " ")
	for _, want := range []string{"Model: gpt-5.6", "Thinking effort: high"} {
		if !strings.Contains(view, want) {
			t.Fatalf("welcome missing %q: %q", want, view)
		}
	}
}

func TestSystemPromptKeepsExplicitLabelsVerbatim(t *testing.T) {
	for _, want := range []string{"label the user supplies", "verbatim", "separate canonical search key"} {
		if !strings.Contains(systemPrompt, want) {
			t.Fatalf("system prompt lost explicit-label rule %q", want)
		}
	}
}
