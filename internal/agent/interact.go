package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"golang.org/x/term"
)

type promptKind uint8

const (
	promptQuestion promptKind = iota
	promptConsent
)

// userPrompt is structured instead of pre-rendered terminal text so
// the active frontend decides how questions look and behave. The TUI
// renders consent as a real choice control; cooked-mode one-shot runs
// retain a compact text fallback.
type userPrompt struct {
	Kind   promptKind
	Title  string
	Detail string
}

// asker is the single input authority for one session: every
// question to the user — consent or ask_user — goes through it.
// nil means the session cannot ask (non-interactive).
//
// This exists because of a real field bug: consent prompts reading a
// shared stdin buffer were instantly "answered" by stale type-ahead
// newlines the user pressed while the model was thinking, denying
// actions nobody saw. One authority, explicit prompts, and a
// drain-and-reprompt protocol make that impossible.
type asker func(context.Context, userPrompt) (string, error)

// stdinAsker returns the cooked-mode asker used by one-shot runs.
// Interactive REPL sessions use the Bubble Tea frontend instead.
// Returns nil when stdin is not a terminal.
func stdinAsker() asker {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil
	}
	return func(_ context.Context, prompt userPrompt) (string, error) {
		// Stale type-ahead already read into the buffer would answer
		// a prompt nobody saw — drop it before asking.
		if n := stdinReader.Buffered(); n > 0 {
			_, _ = stdinReader.Discard(n)
		}
		fmt.Fprint(os.Stderr, renderCookedPrompt(prompt))
		line, err := stdinReader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}
}

func renderCookedPrompt(prompt userPrompt) string {
	switch prompt.Kind {
	case promptConsent:
		return fmt.Sprintf("\n! %s\n  %s\n  Allow? [y/N/a=always this run] ", prompt.Title, prompt.Detail)
	default:
		return fmt.Sprintf("\n? %s\n> ", prompt.Title)
	}
}

// consent runs the y/N/a protocol. Empty answers re-ask (they are
// stale newlines or hesitation, never a decision); "a" allows this
// tool for the rest of the run.
func consent(ctx context.Context, ask asker, st *runState, tool string, prompt userPrompt) agentkit.GateDecision {
	if st.allowAll[tool] {
		return agentkit.GateDecision{Allowed: true}
	}
	if ask == nil {
		return agentkit.GateDecision{
			Allowed: false,
			Reason:  "outside this run's granted scope and the session is non-interactive; report instead, or the user can re-run with --grant auto",
		}
	}
	for tries := 0; tries < 3; tries++ {
		answer, err := ask(ctx, prompt)
		if err != nil {
			return agentkit.GateDecision{Allowed: false, Reason: "no answer from user"}
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "":
			continue // not a decision — ask again
		case "y", "yes":
			return agentkit.GateDecision{Allowed: true}
		case "a", "always":
			st.allowAll[tool] = true
			return agentkit.GateDecision{Allowed: true}
		default:
			return agentkit.GateDecision{Allowed: false, Reason: "user declined"}
		}
	}
	return agentkit.GateDecision{Allowed: false, Reason: "no clear answer from user"}
}
