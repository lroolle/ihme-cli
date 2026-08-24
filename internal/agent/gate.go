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

		// Memory touches only the user's own local notebook — never
		// Apple, never the network. Gating it would nag on every recall
		// and every learning, so it is always allowed. recall reads;
		// remember appends a durable note the user can see and edit.
		case "recall_memory", "remember":
			return agentkit.GateDecision{Allowed: true}

		// refresh_candidates burns a throwaway — a real reserve +
		// delete against Apple. It ran consent-free for three weeks on
		// the "net-zero transient" argument (TASTE.md 2026-07-22);
		// field traces killed that: a model with a miscalibrated taste
		// bar declared healthy pools weak and rotated task after task,
		// and since the per-task cap resets every turn (resetTurn),
		// "capped in code" still meant steady reserve+delete churn —
		// exactly the traffic that draws Apple rate limiting. Net-zero
		// described the ACCOUNT state; the API pressure was never
		// zero. The card shows the model's per-candidate verdict, so
		// the user can veto a bogus weak-pool call ("n" or a redirect)
		// or wave rotation through for the session ("a").
		case "refresh_candidates":
			var args struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(req.Call.Args, &args)
			// Verdict-less churn never reaches the user — same rule as
			// a rationale-less reservation.
			if len(strings.TrimSpace(args.Reason)) < minRationale {
				return agentkit.GateDecision{Allowed: false,
					Reason: "reason first: name each current candidate and its ACTIVE defect (deficit word, leading digits, gibberish) — a pool that is merely plain is not weak: reserve the best instead of rotating"}
			}
			return consent(ctx, ask, st, "refresh_candidates", userPrompt{
				Kind:    promptConsent,
				Title:   "Burn a throwaway to fetch fresh candidates?",
				Subject: st.label,
				Warn:    "a real reserve + delete on Apple — refreshes usually swap only one candidate",
				Why:     strings.TrimSpace(args.Reason),
			})

		case "reserve_address":
			var args struct {
				Address   string      `json:"address"`
				Label     string      `json:"label"`
				Rationale string      `json:"rationale"`
				Rejected  []Rejection `json:"rejected"`
				Note      string      `json:"note"`
				Tags      []string    `json:"tags"`
			}
			_ = json.Unmarshal(req.Call.Args, &args)
			if st.label != "" && st.reserves == 0 && strings.EqualFold(args.Label, st.label) {
				return agentkit.GateDecision{Allowed: true}
			}
			// A consent card without the why is not a decision the
			// user can make — never ask them to approve a verdict-less
			// reservation. Bounce it back to the model instead. (The
			// tool's own length check backstops the GrantAuto path.)
			if len(strings.TrimSpace(args.Rationale)) < minRationale {
				return agentkit.GateDecision{Allowed: false,
					Reason: "rationale first: the user decides from your taste verdict on the consent card — state in one plain sentence why this candidate wins the pool and list each rejected candidate with its failure, then reserve again"}
			}
			// The card shows everything the reservation will write.
			facts := [][2]string{{"label", args.Label}}
			if args.Note != "" {
				facts = append(facts, [2]string{"note", args.Note})
			}
			if len(args.Tags) > 0 {
				facts = append(facts, [2]string{"tags", strings.Join(args.Tags, " · ")})
			}
			var warn string
			switch {
			case st.label == "":
			case !strings.EqualFold(args.Label, st.label):
				warn = fmt.Sprintf("the task label is %q", st.label)
			default:
				warn = "second reservation this run"
			}
			passed := make([][2]string, 0, len(args.Rejected))
			for _, r := range args.Rejected {
				passed = append(passed, [2]string{r.Address, r.Reason})
			}
			return consent(ctx, ask, st, "reserve_address", userPrompt{
				Kind:    promptConsent,
				Title:   "Create this Hide My Email address?",
				Subject: args.Address,
				Facts:   facts,
				Warn:    warn,
				Why:     strings.TrimSpace(args.Rationale),
				Passed:  passed,
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
				Kind:    promptConsent,
				Title:   "Deactivate this address?",
				Subject: args.Ref,
				Warn:    "not created by this run — new mail to it will be rejected",
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
				Kind:    promptConsent,
				Title:   "Update this address?",
				Subject: args.Ref,
				Warn:    "not created by this run — label, note, or tags will change",
			})

		default:
			return agentkit.GateDecision{Allowed: false, Reason: "tool not covered by consent policy"}
		}
	}
}
