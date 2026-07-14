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
)

// stdinReader is shared by the REPL and the consent prompt so a
// buffered read in one never swallows input meant for the other.
var stdinReader = bufio.NewReader(os.Stdin)

const greeting = `ihme agent — what can I help with?

  new address       "new address for github signup"
  find things       "which addresses go to netflix?"
  annotate          "tag my linear address as work"
  clean up          "deactivate the old figma address"

Actions that change anything ask for your consent first
(run with --grant auto to skip the asking). Ctrl-D or "exit" to leave.`

// RunREPL is the interactive general assistant: one persistent
// conversation, fresh budgets per turn, every mutation gated.
func RunREPL(ctx context.Context, svc *app.Service, appleID string, grant GrantMode) error {
	s, err := newSession(svc, appleID, "", grant, os.Stdout)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, greeting)

	transcript := []agentkit.Message{invocation(
		"Assist the user with their Hide My Email addresses in an interactive session. " +
			"Follow the procedure above when creating addresses. Respond to each request " +
			"conversationally and concisely; ask when a request is ambiguous.")}

	for {
		fmt.Fprint(os.Stderr, "\nihme> ")
		line, err := stdinReader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stderr)
				return nil
			}
			return err
		}
		line = strings.TrimSpace(line)
		switch line {
		case "":
			continue
		case "exit", "quit", "q":
			return nil
		}

		transcript = append(transcript, agentkit.Message{Role: agentkit.RoleUser, Text: line})
		updated, err := s.exec(ctx, transcript)
		transcript = updated // keep partial progress even on error
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Keep the session alive: budget trips and model errors
			// end the turn, not the conversation.
			fmt.Fprintf(os.Stderr, "\n(turn ended: %v)\n", err)
		}
	}
}
