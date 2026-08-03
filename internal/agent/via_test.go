package agent

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/internal/acp"
)

func TestResolveVia(t *testing.T) {
	if _, err := resolveVia("gemini"); err == nil {
		t.Error("unsupported guest must be refused")
	}
	argv, err := resolveVia("codex")
	if err != nil || len(argv) == 0 {
		t.Fatalf("codex: %v %v", argv, err)
	}
	// Installed adapter binary or the npx fallback — either way the
	// command must reference the codex adapter.
	if !strings.Contains(strings.Join(argv, " "), "codex") {
		t.Errorf("codex resolves to %v", argv)
	}
}

func TestPickOption(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionID: "ao", Kind: "allow_once"},
		{OptionID: "aa", Kind: "allow_always"},
		{OptionID: "ro", Kind: "reject_once"},
	}
	if got := pickOption(opts, "allow_once", "allow_always"); got != "ao" {
		t.Errorf("allow pick = %q", got)
	}
	if got := pickOption(opts, "allow_always", "allow_once"); got != "aa" {
		t.Errorf("always pick = %q", got)
	}
	// Missing preferred kind falls back within the family, never
	// across it: a reject answer must not select an allow option.
	if got := pickOption([]acp.PermissionOption{{OptionID: "aa", Kind: "allow_always"}}, "allow_once"); got != "aa" {
		t.Errorf("family fallback = %q", got)
	}
	if got := pickOption([]acp.PermissionOption{{OptionID: "aa", Kind: "allow_always"}}, "reject_once", "reject_always"); got != "" {
		t.Errorf("reject must not fall back to allow, got %q", got)
	}
}

func TestConsentPromptReserveCard(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"address": "glen_arbor@icloud.com", "label": "github",
		"rationale": "a northern lakeshore town — a place on a map",
		"rejected":  []map[string]any{{"address": "63.fryer@icloud.com", "reason": "serial number"}},
		"note":      "signup", "tags": []string{"dev"},
	})
	p := consentPrompt(acp.Update{Title: "mcp__ihme__reserve_address", RawInput: raw})
	if p.Subject != "glen_arbor@icloud.com" {
		t.Errorf("subject = %q — the address must be the decision subject", p.Subject)
	}
	if !strings.Contains(p.Why, "lakeshore") {
		t.Errorf("rationale missing from card: %q", p.Why)
	}
	if len(p.Passed) != 1 || p.Passed[0][0] != "63.fryer@icloud.com" {
		t.Errorf("rejected candidates missing: %v", p.Passed)
	}

	// Foreign tools get an honest generic card, not a blank one.
	g := consentPrompt(acp.Update{Title: "shell", RawInput: json.RawMessage(`{"command":"rm -rf /"}`)})
	if g.Subject != "shell" || len(g.Facts) == 0 {
		t.Errorf("generic card = %+v", g)
	}
}

func TestViaRunCaptureReserve(t *testing.T) {
	res := &ViaResult{}
	v := &viaRun{res: res, meta: io.Discard, titles: map[string]string{}, text: mdPassthrough{w: io.Discard}}

	input, _ := json.Marshal(map[string]any{
		"rationale": "the picture holds", "rejected": []map[string]any{{"address": "x@icloud.com", "reason": "no image"}},
	})
	// Adapter-enveloped MCP result: JSON inside content[0].text.
	inner, _ := json.Marshal(map[string]any{"status": "reserved",
		"address": map[string]any{"anonymousId": "abc123", "label": "github", "hme": "glen_arbor@icloud.com", "isActive": true}})
	output, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": string(inner)}}})

	v.update(acp.Update{Kind: "tool_call", ToolCallID: "t1", Title: "mcp__ihme__reserve_address", RawInput: input})
	v.update(acp.Update{Kind: "tool_call_update", ToolCallID: "t1", Status: "completed", RawInput: input, RawOutput: output})

	if res.Reserved == nil || res.Reserved.Hme != "glen_arbor@icloud.com" {
		t.Fatalf("reservation not captured: %+v", res.Reserved)
	}
	if res.Rationale != "the picture holds" || len(res.Rejected) != 1 {
		t.Errorf("verdict not captured: %q %v", res.Rationale, res.Rejected)
	}

	// Bare (non-enveloped) result must parse too.
	res2 := &ViaResult{}
	v2 := &viaRun{res: res2, meta: io.Discard, titles: map[string]string{}, text: mdPassthrough{w: io.Discard}}
	v2.update(acp.Update{Kind: "tool_call", ToolCallID: "t1", Title: "reserve_address", RawInput: input})
	v2.update(acp.Update{Kind: "tool_call_update", ToolCallID: "t1", Status: "completed", RawInput: input, RawOutput: inner})
	if res2.Reserved == nil {
		t.Fatal("bare rawOutput not captured")
	}
}

func TestViaPermissionModes(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionID: "ao", Kind: "allow_once"},
		{OptionID: "ro", Kind: "reject_once"},
	}
	req := acp.PermissionRequest{Options: opts, ToolCall: acp.Update{Title: "reserve_address"}}

	auto := &viaRun{res: &ViaResult{}, grant: GrantAuto, meta: io.Discard, titles: map[string]string{}}
	if got := auto.permission(req); got != "ao" {
		t.Errorf("GrantAuto = %q, want allow", got)
	}

	// Non-interactive without auto: reject, mirroring the embedded
	// gate's non-interactive denial.
	unattended := &viaRun{res: &ViaResult{}, grant: GrantAsk, ask: nil, meta: io.Discard, titles: map[string]string{}}
	if got := unattended.permission(req); got != "ro" {
		t.Errorf("non-interactive ask = %q, want reject", got)
	}

	// Typed reply: reject AND queue the redirect for the next turn.
	replies := []string{"try shorter ones"}
	scripted := &viaRun{res: &ViaResult{}, grant: GrantAsk, meta: io.Discard, titles: map[string]string{},
		ask: func(_ context.Context, _ userPrompt) (string, error) {
			r := replies[0]
			return r, nil
		}}
	if got := scripted.permission(req); got != "ro" {
		t.Errorf("typed reply = %q, want reject_once", got)
	}
	if got := scripted.takeRedirect(); got != "try shorter ones" {
		t.Errorf("redirect = %q", got)
	}
	if got := scripted.takeRedirect(); got != "" {
		t.Errorf("redirect must be consumed once, got %q", got)
	}
}
