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
			detail := fmt.Sprintf("Reserve %s with label %q. This creates a new address in your iCloud account.",
				args.Address, args.Label)
			switch {
			case st.label == "":
			case !strings.EqualFold(args.Label, st.label):
				detail = fmt.Sprintf("Reserve %s with label %q. The task label is %q.",
					args.Address, args.Label, st.label)
			default:
				detail = fmt.Sprintf("Reserve %s with label %q. This is the second reservation in this run.",
					args.Address, args.Label)
			}
			return consent(ctx, ask, st, "reserve_address", userPrompt{
				Kind:   promptConsent,
				Title:  "Create this Hide My Email address?",
				Detail: detail,
			})

		case "deactivate_address":
			var args struct {
				Ref string `json:"ref"`
			}
			_ = json.Unmarshal(req.Call.Args, &args)
			if st.ownedRef(args.Ref) {
				return agentkit.GateDecision{Allowed: true}
			}
			return consent(ctx, ask, st, "deactivate_address", userPrompt{
				Kind:   promptConsent,
				Title:  "Deactivate this address?",
				Detail: fmt.Sprintf("%q was not created by this run. New mail to it will be rejected.", args.Ref),
			})

		case "edit_note":
			var args struct {
				Ref string `json:"ref"`
			}
			_ = json.Unmarshal(req.Call.Args, &args)
			if st.ownedRef(args.Ref) {
				return agentkit.GateDecision{Allowed: true}
			}
			return consent(ctx, ask, st, "edit_note", userPrompt{
				Kind:   promptConsent,
				Title:  "Update this address?",
				Detail: fmt.Sprintf("Edit metadata for %q, which was not created by this run.", args.Ref),
			})

		default:
			return agentkit.GateDecision{Allowed: false, Reason: "tool not covered by consent policy"}
		}
	}
}
