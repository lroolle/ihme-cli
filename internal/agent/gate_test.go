package agent

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

// Tests run non-interactively (stdin is not a TTY), so prompt()
// denies — which is exactly the unattended behavior under test.

func decide(t *testing.T, st *runState, tool, args string) agentkit.GateDecision {
	t.Helper()
	g := gate(GrantAsk, st, nil) // nil asker = non-interactive
	return g(context.Background(), agentkit.GateRequest{
		Turn: 1,
		Call: agentkit.ToolCall{Name: tool, Args: json.RawMessage(args)},
	})
}

func TestReadsAlwaysAllowed(t *testing.T) {
	st := newRunState("github")
	for _, tool := range []string{"auth_status", "search_addresses", "generate_candidates"} {
		if d := decide(t, st, tool, `{}`); !d.Allowed {
			t.Fatalf("%s denied: %s", tool, d.Reason)
		}
	}
}

func TestFirstReserveForTaskLabelPreGranted(t *testing.T) {
	st := newRunState("github")
	d := decide(t, st, "reserve_address", `{"address":"a@icloud.com","label":"GitHub"}`)
	if !d.Allowed {
		t.Fatalf("first reserve for task label denied: %s", d.Reason)
	}
}

func TestSecondReserveNotPreGranted(t *testing.T) {
	st := newRunState("github")
	st.recordReservation(&api.HmeEmail{Hme: "a@icloud.com", AnonymousID: "id1"})
	d := decide(t, st, "reserve_address", `{"address":"b@icloud.com","label":"github"}`)
	if d.Allowed {
		t.Fatal("second reserve must not be pre-granted")
	}
	if d.Reason == "" {
		t.Fatal("denial must carry a reason for the model")
	}
}

func TestReserveForOtherLabelNotPreGranted(t *testing.T) {
	st := newRunState("github")
	d := decide(t, st, "reserve_address", `{"address":"a@icloud.com","label":"netflix"}`)
	if d.Allowed {
		t.Fatal("reserve under a different label must not be pre-granted")
	}
}

func TestDeactivateOwnPlacementAllowed(t *testing.T) {
	st := newRunState("github")
	st.recordReservation(&api.HmeEmail{Hme: "a@icloud.com", AnonymousID: "id1"})
	if d := decide(t, st, "deactivate_address", `{"ref":"a@icloud.com"}`); !d.Allowed {
		t.Fatalf("deactivating own placeholder denied: %s", d.Reason)
	}
	if d := decide(t, st, "deactivate_address", `{"ref":"ID1"}`); !d.Allowed {
		t.Fatalf("own anonymousId (case-insensitive) denied: %s", d.Reason)
	}
}

func TestDeactivateForeignAddressDenied(t *testing.T) {
	st := newRunState("github")
	if d := decide(t, st, "deactivate_address", `{"ref":"precious@icloud.com"}`); d.Allowed {
		t.Fatal("deactivating a pre-existing address must not be pre-granted")
	}
}

func TestUnknownToolDeniedByDefault(t *testing.T) {
	st := newRunState("github")
	if d := decide(t, st, "future_tool", `{}`); d.Allowed {
		t.Fatal("unknown tools must be denied by the consent policy")
	}
}

// Refresh burns a real reserve+delete against Apple; field traces
// showed miscalibrated models rotating healthy pools task after task
// (TASTE.md 2026-08-12 reversal), so it is consent-gated like any
// other Apple mutation — and a verdict-less refresh never reaches
// the user at all.
func TestRefreshWithoutReasonNeverReachesTheUser(t *testing.T) {
	st := newRunState("github")
	asked := false
	ask := func(context.Context, userPrompt) (string, error) {
		asked = true
		return "y", nil
	}
	g := gate(GrantAsk, st, ask)
	d := g(context.Background(), agentkit.GateRequest{
		Call: agentkit.ToolCall{Name: "refresh_candidates", Args: json.RawMessage(`{}`)},
	})
	if d.Allowed || asked {
		t.Fatalf("verdict-less refresh must bounce to the model, not the user (allowed=%v asked=%v)", d.Allowed, asked)
	}
	if !strings.Contains(d.Reason, "reason") {
		t.Fatalf("denial must tell the model what to fix: %q", d.Reason)
	}
}

func TestRefreshAsksConsentAndCarriesTheVerdict(t *testing.T) {
	st := newRunState("github")
	var got userPrompt
	ask := func(_ context.Context, p userPrompt) (string, error) {
		got = p
		return "y", nil
	}
	g := gate(GrantAsk, st, ask)
	d := g(context.Background(), agentkit.GateRequest{
		Call: agentkit.ToolCall{Name: "refresh_candidates",
			Args: json.RawMessage(`{"reason":"63.posher leads with digits; debts_pale carries a deficit word; qzx.vrn is gibberish"}`)},
	})
	if !d.Allowed {
		t.Fatalf("consented refresh denied: %s", d.Reason)
	}
	if !strings.Contains(got.Why, "leads with digits") {
		t.Fatalf("per-candidate verdict missing from the card: %q", got.Why)
	}
}

func TestRefreshUnattendedDenied(t *testing.T) {
	st := newRunState("github")
	d := decide(t, st, "refresh_candidates",
		`{"reason":"63.posher leads with digits; debts_pale carries a deficit word; qzx.vrn is gibberish"}`)
	if d.Allowed {
		t.Fatal("refresh must not run without consent in a non-interactive session")
	}
}

func TestResetTurnClearsRateCapsButKeepsScope(t *testing.T) {
	st := newRunState("github")
	st.generateRounds = 3
	st.questions = 3
	st.refreshes = 2
	st.recordReservation(&api.HmeEmail{Hme: "a@icloud.com", AnonymousID: "id1"})
	st.allowAll["deactivate_address"] = true

	st.resetTurn()

	if st.generateRounds != 0 || st.questions != 0 || st.refreshes != 0 {
		t.Fatalf("per-task caps not reset: %+v", st)
	}
	// Ownership and session consent must survive into the next turn.
	if !st.ownedRef("a@icloud.com") {
		t.Fatal("reset dropped ownership — a created address stopped being owned mid-session")
	}
	if !st.allowAll["deactivate_address"] {
		t.Fatal("reset dropped an 'always this run' consent grant")
	}
}

func TestAutoModeSkipsGate(t *testing.T) {
	if g := gate(GrantAuto, newRunState("github"), nil); g != nil {
		t.Fatal("auto mode should return a nil gate (allow all)")
	}
}

// scriptedAsker returns queued answers in order.
func scriptedAsker(answers ...string) asker {
	i := 0
	return func(_ context.Context, _ userPrompt) (string, error) {
		if i >= len(answers) {
			return "", io.EOF
		}
		a := answers[i]
		i++
		return a, nil
	}
}

func TestConsentProtocol(t *testing.T) {
	cases := []struct {
		name    string
		answers []string
		allow   bool
	}{
		{"yes", []string{"y"}, true},
		{"YES", []string{"YES"}, true},
		{"no", []string{"n"}, false},
		{"anything else declines", []string{"whatever"}, false},
		{"stale empty lines re-ask, then yes", []string{"", "", "y"}, true},
		{"only empties gives up as deny", []string{"", "", ""}, false},
	}
	for _, tc := range cases {
		st := newRunState("x")
		d := consent(context.Background(), scriptedAsker(tc.answers...), st, "reserve_address", userPrompt{Kind: promptConsent, Title: "Reserve x?"})
		if d.Allowed != tc.allow {
			t.Fatalf("%s: allowed = %v, want %v (%s)", tc.name, d.Allowed, tc.allow, d.Reason)
		}
	}
}

func TestConsentAlwaysRemembersPerTool(t *testing.T) {
	st := newRunState("x")
	if d := consent(context.Background(), scriptedAsker("a"), st, "deactivate_address", userPrompt{Kind: promptConsent, Title: "first?"}); !d.Allowed {
		t.Fatal("'a' must allow")
	}
	// Second time: no asker needed at all.
	if d := consent(context.Background(), nil, st, "deactivate_address", userPrompt{Kind: promptConsent, Title: "second?"}); !d.Allowed {
		t.Fatal("allow-all not remembered")
	}
	// Other tools are unaffected.
	if d := consent(context.Background(), nil, st, "edit_note", userPrompt{Kind: promptConsent, Title: "other?"}); d.Allowed {
		t.Fatal("allow-all leaked across tools")
	}
}

func TestConsentNonInteractiveDenies(t *testing.T) {
	st := newRunState("x")
	d := consent(context.Background(), nil, st, "reserve_address", userPrompt{Kind: promptConsent, Title: "x?"})
	if d.Allowed || !strings.Contains(d.Reason, "non-interactive") {
		t.Fatalf("d = %+v", d)
	}
}

// The consent card is a decision surface: it must carry the verdict,
// and a verdict-less reservation must never reach the user at all.
func TestReserveWithoutRationaleNeverReachesTheUser(t *testing.T) {
	st := newRunState("")
	asked := false
	ask := func(context.Context, userPrompt) (string, error) {
		asked = true
		return "y", nil
	}
	g := gate(GrantAsk, st, ask)
	d := g(context.Background(), agentkit.GateRequest{
		Call: agentkit.ToolCall{Name: "reserve_address",
			Args: json.RawMessage(`{"address":"a@icloud.com","label":"x","rationale":"nice"}`)},
	})
	if d.Allowed || asked {
		t.Fatalf("verdict-less reserve must bounce to the model, not the user (allowed=%v asked=%v)", d.Allowed, asked)
	}
	if !strings.Contains(d.Reason, "rationale") {
		t.Fatalf("denial must tell the model what to fix: %q", d.Reason)
	}
}

func TestReserveConsentCarriesTheVerdict(t *testing.T) {
	st := newRunState("")
	var got userPrompt
	ask := func(_ context.Context, p userPrompt) (string, error) {
		got = p
		return "y", nil
	}
	g := gate(GrantAsk, st, ask)
	d := g(context.Background(), agentkit.GateRequest{
		Call: agentkit.ToolCall{Name: "reserve_address",
			Args: json.RawMessage(`{"address":"calm.river@icloud.com","label":"grok","note":"grok signup","tags":["ai"],"rationale":"a calm river bend over gravel — kept for the image","rejected":[{"address":"turbo3_placard","reason":"embedded digit"},{"address":"pale_slate","reason":"no image"}]}`)},
	})
	if !d.Allowed {
		t.Fatalf("consented reserve denied: %s", d.Reason)
	}
	if got.Subject != "calm.river@icloud.com" {
		t.Fatalf("subject = %q", got.Subject)
	}
	if !strings.Contains(got.Why, "river bend") {
		t.Fatalf("verdict missing from consent prompt: %q", got.Why)
	}
	wantFacts := map[string]string{"label": "grok", "note": "grok signup", "tags": "ai"}
	for _, fact := range got.Facts {
		if wantFacts[fact[0]] != fact[1] {
			t.Fatalf("fact %q = %q, want %q", fact[0], fact[1], wantFacts[fact[0]])
		}
		delete(wantFacts, fact[0])
	}
	if len(wantFacts) > 0 {
		t.Fatalf("facts missing from the card: %v (got %v)", wantFacts, got.Facts)
	}
	if len(got.Passed) != 2 || got.Passed[0][0] != "turbo3_placard" || got.Passed[1][1] != "no image" {
		t.Fatalf("passed-on list wrong: %v", got.Passed)
	}
}

func TestConsentReplyBecomesGuidance(t *testing.T) {
	st := newRunState("x")
	d := consent(context.Background(), scriptedAsker("use calm_mule instead, and tag it work"), st,
		"reserve_address", userPrompt{Kind: promptConsent, Title: "Create?"})
	if d.Allowed {
		t.Fatal("a reply is not an approval")
	}
	if !strings.Contains(d.Reason, "calm_mule instead, and tag it work") || !strings.Contains(d.Reason, "direction") {
		t.Fatalf("user's words must reach the model as direction: %q", d.Reason)
	}
}

// General assistant scope (empty label): nothing is pre-granted —
// even the first reservation asks.
func TestGeneralScopeFirstReserveAsks(t *testing.T) {
	st := newRunState("")
	d := decide(t, st, "reserve_address", `{"address":"a@icloud.com","label":"github"}`)
	if d.Allowed {
		t.Fatal("general scope must not pre-grant reservations")
	}
	// But its own creations remain touchable.
	st.recordReservation(&api.HmeEmail{Hme: "a@icloud.com", AnonymousID: "id1"})
	if d := decide(t, st, "edit_note", `{"ref":"a@icloud.com"}`); !d.Allowed {
		t.Fatalf("editing own creation denied: %s", d.Reason)
	}
}

func TestAskUserToolOnlyWhenInteractive(t *testing.T) {
	st := newRunState("x")
	names := func(ts []agentkit.Tool) map[string]bool {
		m := map[string]bool{}
		for _, tool := range ts {
			m[tool.Name()] = true
		}
		return m
	}
	if names(tools(nil, st, "a@b", nil, nil))["ask_user"] {
		t.Fatal("ask_user must not exist in non-interactive runs")
	}
	if !names(tools(nil, st, "a@b", scriptedAsker(), nil))["ask_user"] {
		t.Fatal("ask_user missing in interactive runs")
	}
	if d := decide(t, st, "ask_user", `{"question":"which?"}`); !d.Allowed {
		t.Fatalf("gate must allow ask_user: %s", d.Reason)
	}
}

func TestAskUserBudgetAndAnswer(t *testing.T) {
	st := newRunState("x")
	var ask agentkit.Tool
	for _, tool := range tools(nil, st, "a@b", scriptedAsker("the pro account"), nil) {
		if tool.Name() == "ask_user" {
			ask = tool
		}
	}
	out, err := ask.Execute(context.Background(), json.RawMessage(`{"question":"which account?"}`))
	if err != nil || !strings.Contains(string(out), "the pro account") {
		t.Fatalf("out=%s err=%v", out, err)
	}
	st.questions = maxQuestions
	if _, err := ask.Execute(context.Background(), json.RawMessage(`{"question":"again?"}`)); err == nil {
		t.Fatal("question budget not enforced")
	}
}
