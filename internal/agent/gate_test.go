package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

// Tests run non-interactively (stdin is not a TTY), so prompt()
// denies — which is exactly the unattended behavior under test.

func decide(t *testing.T, st *runState, tool, args string) agentkit.GateDecision {
	t.Helper()
	g := gate(GrantAsk, st)
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

func TestAutoModeSkipsGate(t *testing.T) {
	if g := gate(GrantAuto, newRunState("github")); g != nil {
		t.Fatal("auto mode should return a nil gate (allow all)")
	}
}

func TestPromptParsing(t *testing.T) {
	cases := []struct {
		in    string
		allow bool
	}{
		{"y\n", true}, {"YES\n", true}, {"n\n", false}, {"\n", false}, {"whatever\n", false},
	}
	for _, tc := range cases {
		var out strings.Builder
		d := promptWith(strings.NewReader(tc.in), &out, true, "Reserve x?")
		if d.Allowed != tc.allow {
			t.Fatalf("input %q: allowed = %v, want %v", tc.in, d.Allowed, tc.allow)
		}
		if !strings.Contains(out.String(), "[y/N]") {
			t.Fatal("prompt not written")
		}
	}
	if d := promptWith(strings.NewReader("y\n"), &strings.Builder{}, false, "x"); d.Allowed {
		t.Fatal("non-interactive must deny regardless of stdin content")
	}
}
