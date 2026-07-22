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
			Kind:   promptConsent,
			Title:  "Create this Hide My Email address?",
			Detail: "Reserve calm.river@icloud.com with label \"grok\".",
		},
		reply: replies,
	})
	if m.phase != phaseConsent || m.choice != 1 {
		t.Fatalf("phase=%v choice=%d", m.phase, m.choice)
	}
	view := m.View().Content
	for _, want := range []string{"Allow once", "Deny", "Always this run", "y/n/a shortcut"} {
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

func TestSystemPromptKeepsExplicitLabelsVerbatim(t *testing.T) {
	for _, want := range []string{"label the user supplies", "verbatim", "separate canonical search key"} {
		if !strings.Contains(systemPrompt, want) {
			t.Fatalf("system prompt lost explicit-label rule %q", want)
		}
	}
}
