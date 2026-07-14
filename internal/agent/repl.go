package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"golang.org/x/term"
)

// stdinReader is shared by the cooked-mode paths (one-shot consent
// and the piped REPL) so buffered reads never race each other.
var stdinReader = bufio.NewReader(os.Stdin)

const greeting = `ihme agent — what can I help with?

  new address       "new address for github signup"
  find things       "which addresses go to netflix?"
  annotate          "tag my linear address as work"
  clean up          "deactivate the old figma address"

Actions that change anything ask first: y allows once, a allows that
action for the rest of the run. Up-arrow recalls history. Ctrl-D or
"exit" leaves.`

const replPrompt = "ihme> "

// RunREPL is the interactive general assistant: one persistent
// conversation, fresh budgets per turn, every mutation gated.
//
// On a terminal it runs a raw-mode line editor (x/term.Terminal):
// history, editing, and — the important part — no phantom answers.
// Keys typed while the model is working become visible, editable
// input for the NEXT prompt instead of silently answering a consent
// question nobody saw (a real field bug with cooked-mode stdin).
func RunREPL(ctx context.Context, svc *app.Service, appleID string, grant GrantMode, effort string) error {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return runPipedREPL(ctx, svc, appleID, grant, effort)
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("entering raw mode: %w", err)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	t := term.NewTerminal(stdinStderr{}, replPrompt)
	// Degenerate PTYs report zero columns; a zero-width terminal
	// wraps every rune. Keep the editor's 80-column default then.
	if width, height, err := term.GetSize(fd); err == nil && width > 0 {
		_ = t.SetSize(width, max(height, 1))
	}
	ask := func(prompt string) (string, error) {
		t.SetPrompt(prompt)
		defer t.SetPrompt(replPrompt)
		return t.ReadLine()
	}

	s, err := newSession(svc, appleID, "", grant, effort, sessionIO{textOut: t, meta: t, ask: ask})
	if err != nil {
		return err
	}
	fmt.Fprintln(t, greeting)

	transcript := startTranscript()
	for {
		line, err := t.ReadLine() // io.EOF on Ctrl-C and Ctrl-D
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var done bool
		transcript, done = replTurn(ctx, s, transcript, line, t)
		if done {
			return ctx.Err()
		}
	}
}

// runPipedREPL serves non-terminal stdin (scripts, tests): plain
// line loop, no editor, consent auto-denies with a reason.
func runPipedREPL(ctx context.Context, svc *app.Service, appleID string, grant GrantMode, effort string) error {
	s, err := newSession(svc, appleID, "", grant, effort, sessionIO{textOut: os.Stdout, meta: os.Stderr, ask: nil})
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, greeting)

	transcript := startTranscript()
	for {
		fmt.Fprint(os.Stderr, "\n"+replPrompt)
		line, err := stdinReader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stderr)
				return nil
			}
			return err
		}
		var done bool
		transcript, done = replTurn(ctx, s, transcript, line, os.Stderr)
		if done {
			return ctx.Err()
		}
	}
}

func startTranscript() []agentkit.Message {
	return []agentkit.Message{invocation(
		"Assist the user with their Hide My Email addresses in an interactive session. " +
			"Follow the procedure above when creating addresses. Respond to each request " +
			"conversationally and concisely; ask when a request is ambiguous.")}
}

// replTurn runs one user line through the session. done reports that
// the conversation should end (exit words or context cancellation).
func replTurn(ctx context.Context, s *session, transcript []agentkit.Message, line string, errOut io.Writer) ([]agentkit.Message, bool) {
	line = strings.TrimSpace(line)
	switch line {
	case "":
		return transcript, false
	case "exit", "quit", "q":
		return transcript, true
	}

	transcript = append(transcript, agentkit.Message{Role: agentkit.RoleUser, Text: line})
	updated, err := s.exec(ctx, transcript)
	transcript = updated // keep partial progress even on error
	if err != nil {
		if ctx.Err() != nil {
			return transcript, true
		}
		// Budget trips and model errors end the turn, not the session.
		fmt.Fprintf(errOut, "\n(turn ended: %v)\n", err)
	}
	return transcript, false
}

// stdinStderr is the ReadWriter the line editor drives: input from
// the raw terminal, output (with edit-line coordination and CRLF
// mapping) to stderr, keeping stdout clean for data.
type stdinStderr struct{}

func (stdinStderr) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdinStderr) Write(p []byte) (int, error) { return os.Stderr.Write(p) }
