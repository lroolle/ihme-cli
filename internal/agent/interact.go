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
	Kind    promptKind
	Title   string
	Subject string      // the thing being acted on — rendered prominent
	Facts   [][2]string // what gets written: label, note, tags — key/value
	Warn    string      // scope warning (second reservation, foreign address)
	Why     string      // the agent's verdict on the winner, in its own words
	Passed  [][2]string // candidate → why it lost, one row per rejected
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
		var b strings.Builder
		fmt.Fprintf(&b, "\n! %s\n", prompt.Title)
		if prompt.Subject != "" {
			fmt.Fprintf(&b, "  \x1b[1m%s\x1b[0m\n", prompt.Subject)
		}
		for _, fact := range prompt.Facts {
			fmt.Fprintf(&b, "  %-5s  %s\n", fact[0], fact[1])
		}
		if prompt.Warn != "" {
			fmt.Fprintf(&b, "  ⚠ %s\n", prompt.Warn)
		}
		if prompt.Why != "" {
			fmt.Fprintf(&b, "\n  %s\n", prompt.Why)
		}
		if len(prompt.Passed) > 0 {
			b.WriteString("\n  passed on:\n")
			for _, p := range prompt.Passed {
				fmt.Fprintf(&b, "    %s — %s\n", p[0], p[1])
			}
		}
		b.WriteString("\n  Allow? [y/N/a=always, or type a reply to redirect] ")
		return b.String()
	default:
		return fmt.Sprintf("\n? %s\n> ", prompt.Title)
	}
}

// consent runs the y/N/a protocol. Empty answers re-ask (they are
// stale newlines or hesitation, never a decision); "a" allows this
// tool for the rest of the run. Any other text is the user talking
// back — it rides the denial reason to the model as direction.
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
		text := strings.TrimSpace(answer)
		switch strings.ToLower(text) {
		case "":
			continue // not a decision — ask again
		case "y", "yes":
			return agentkit.GateDecision{Allowed: true}
		case "a", "always":
			st.allowAll[tool] = true
			return agentkit.GateDecision{Allowed: true}
		case "n", "no":
			return agentkit.GateDecision{Allowed: false, Reason: "user declined"}
		default:
			return agentkit.GateDecision{Allowed: false,
				Reason: fmt.Sprintf("user replied instead of approving: %q — this is direction, not rejection of the task: adapt (different candidate, another round, changed metadata) and continue", text)}
		}
	}
	return agentkit.GateDecision{Allowed: false, Reason: "no clear answer from user"}
}
