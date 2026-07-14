package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"golang.org/x/term"
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
// including a second reservation. Apple-sourced strings in the
// transcript are untrusted; nothing they say widens this scope.
func gate(mode GrantMode, st *runState) agentkit.Gate {
	if mode == GrantAuto {
		return nil
	}
	return func(ctx context.Context, req agentkit.GateRequest) agentkit.GateDecision {
		switch req.Call.Name {
		case "auth_status", "search_addresses", "generate_candidates":
			return agentkit.GateDecision{Allowed: true}

		case "reserve_address":
			var args struct {
				Address string `json:"address"`
				Label   string `json:"label"`
			}
			_ = json.Unmarshal(req.Call.Args, &args)
			if st.reserves == 0 && strings.EqualFold(args.Label, st.label) {
				return agentkit.GateDecision{Allowed: true}
			}
			why := "a second reservation this run"
			if !strings.EqualFold(args.Label, st.label) {
				why = fmt.Sprintf("a reservation under label %q (task label is %q)", args.Label, st.label)
			}
			return prompt(fmt.Sprintf("Agent wants %s: reserve %s as %q.", why, args.Address, args.Label))

		case "deactivate_address":
			var args struct {
				Ref string `json:"ref"`
			}
			_ = json.Unmarshal(req.Call.Args, &args)
			if st.ownedRef(args.Ref) {
				return agentkit.GateDecision{Allowed: true}
			}
			return prompt(fmt.Sprintf("Agent wants to deactivate %q — NOT created by this run.", args.Ref))

		case "edit_note":
			var args struct {
				Ref string `json:"ref"`
			}
			_ = json.Unmarshal(req.Call.Args, &args)
			if st.ownedRef(args.Ref) {
				return agentkit.GateDecision{Allowed: true}
			}
			return prompt(fmt.Sprintf("Agent wants to edit %q — NOT created by this run.", args.Ref))

		default:
			return agentkit.GateDecision{Allowed: false, Reason: "tool not covered by consent policy"}
		}
	}
}

// prompt asks on stderr and reads stdin. The loop is synchronous, so
// this never interleaves with stream rendering. Non-interactive
// sessions deny with a reason the model can act on.
func prompt(what string) agentkit.GateDecision {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return agentkit.GateDecision{
			Allowed: false,
			Reason:  "outside this run's granted scope and the session is non-interactive; report instead, or the user can re-run with --grant auto",
		}
	}
	fmt.Fprintf(os.Stderr, "\n%s Allow? [y/N] ", what)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return agentkit.GateDecision{Allowed: false, Reason: "no answer from user"}
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return agentkit.GateDecision{Allowed: true}
	default:
		return agentkit.GateDecision{Allowed: false, Reason: "user declined"}
	}
}
