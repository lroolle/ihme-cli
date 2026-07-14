package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

// GrantMode selects the consent policy for one run.
type GrantMode string

const (
	// GrantAsk applies scoped consent: the user's command pre-grants
	// exactly its own scope; everything else prompts interactively.
	GrantAsk GrantMode = "ask"
	// GrantAuto allows every tool call without prompting.
	GrantAuto GrantMode = "auto"
)

// gate builds the scoped-consent gate. The invocation
// `ihme new <label> --agent` pre-grants: reserving ONE address for
// that label, and deactivating/editing addresses created THIS run
// (rotation). Reads are always allowed. Anything else prompts —
// including a second reservation. In the general assistant
// (st.label empty — `ihme agent`) nothing is pre-granted except the
// run's own creations: EVERY first mutation asks. Apple-sourced
// strings in the transcript are untrusted; nothing they say widens
// this scope.
func gate(mode GrantMode, st *runState, ask asker) agentkit.Gate {
	if mode == GrantAuto {
		return nil
	}
	return func(ctx context.Context, req agentkit.GateRequest) agentkit.GateDecision {
		switch req.Call.Name {
		case "auth_status", "search_addresses", "generate_candidates", "ask_user":
			return agentkit.GateDecision{Allowed: true}

		case "reserve_address":
			var args struct {
				Address string `json:"address"`
				Label   string `json:"label"`
			}
			_ = json.Unmarshal(req.Call.Args, &args)
			if st.label != "" && st.reserves == 0 && strings.EqualFold(args.Label, st.label) {
				return agentkit.GateDecision{Allowed: true}
			}
			why := "a reservation"
			switch {
			case st.label == "":
			case !strings.EqualFold(args.Label, st.label):
				why = fmt.Sprintf("a reservation under label %q (task label is %q)", args.Label, st.label)
			default:
				why = "a second reservation this run"
			}
			return consent(ask, st, "reserve_address",
				fmt.Sprintf("Agent wants %s: reserve %s as %q.", why, args.Address, args.Label))

		case "deactivate_address":
			var args struct {
				Ref string `json:"ref"`
			}
			_ = json.Unmarshal(req.Call.Args, &args)
			if st.ownedRef(args.Ref) {
				return agentkit.GateDecision{Allowed: true}
			}
			return consent(ask, st, "deactivate_address",
				fmt.Sprintf("Agent wants to deactivate %q — NOT created by this run.", args.Ref))

		case "edit_note":
			var args struct {
				Ref string `json:"ref"`
			}
			_ = json.Unmarshal(req.Call.Args, &args)
			if st.ownedRef(args.Ref) {
				return agentkit.GateDecision{Allowed: true}
			}
			return consent(ask, st, "edit_note",
				fmt.Sprintf("Agent wants to edit %q — NOT created by this run.", args.Ref))

		default:
			return agentkit.GateDecision{Allowed: false, Reason: "tool not covered by consent policy"}
		}
	}
}
