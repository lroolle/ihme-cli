package cmdutil

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/pkg/output"
	"github.com/spf13/cobra"
)

// validateTTL is how long a confirmed session is trusted without a
// fresh validate round trip. Within the window commands start
// immediately on saved cookies; past it, the session is revalidated
// (transiently-failing validates surface as "try again", not
// "re-login").
const validateTTL = 15 * time.Minute

// ErrNotLoggedIn is the no-session-on-disk case. Nothing to recover
// from and nothing to retry: only a login fixes it.
var ErrNotLoggedIn = errors.New("no saved session")

func GetClient(cmd *cobra.Command) (*api.Client, error) {
	sessPath := api.DefaultSessionPath()
	sess, err := api.LoadSession(sessPath)
	if err != nil {
		return nil, fmt.Errorf("loading session: %w", err)
	}
	if sess == nil {
		return nil, ErrNotLoggedIn
	}

	client, err := api.NewClientWithSession(sess)
	if err != nil {
		return nil, err
	}
	client.Verbose, _ = cmd.Flags().GetBool("verbose")

	// A session re-minted mid-command (a service host rejected the
	// call the pre-flight check had cleared) is worth keeping: the
	// next command starts on the fresh cookies instead of paying for
	// the same recovery again.
	client.OnSessionUpdate = func(sess *api.SessionData) {
		_ = api.SaveSession(sessPath, sess)
	}

	// Recently confirmed sessions skip the validate round trip.
	if time.Since(sess.ValidatedAt) < validateTTL {
		return client, nil
	}

	if err := client.ResumeSession(); err != nil {
		return nil, err
	}

	// Save updated session (cookies + validation timestamp refreshed)
	api.SaveSession(sessPath, client.Session())

	return client, nil
}

// Explain renders err the way the user should read it: what
// happened, the cause worth pasting into a bug report, and the one
// command that fixes it. Apple's transport failures and Apple's
// verdicts on the session look alike in a stack of wrapped errors
// and are opposite advice — "try again" versus "sign in again" —
// so the split happens here, once, for every command.
func Explain(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNotLoggedIn):
		return "Error: not signed in to iCloud\n  Fix: ihme auth login"
	case api.IsAuthRejection(err):
		return fmt.Sprintf("Error: iCloud rejected this session — the saved login is no longer valid\n  Cause: %s\n  Fix: ihme auth login", err)
	case api.IsTransient(err):
		return fmt.Sprintf("Error: iCloud is temporarily unreachable — your session is probably still valid\n  Cause: %s\n  Fix: run the same command again in a moment", err)
	default:
		return fmt.Sprintf("Error: %s", err)
	}
}

// ExitCode maps err onto the documented contract: 2 means the user
// must authenticate, 1 is every other failure. Scripts and agents
// branch on this instead of matching message text.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrNotLoggedIn), api.IsAuthRejection(err):
		return 2
	default:
		return 1
	}
}

func OutputResult(cmd *cobra.Command, v any) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	jqExpr, _ := cmd.Flags().GetString("jq")

	if jqExpr != "" {
		return output.PrintJQ(os.Stdout, v, jqExpr)
	}
	if jsonFlag {
		return output.PrintJSON(os.Stdout, v)
	}

	switch data := v.(type) {
	case []api.HmeEmail:
		output.PrintTable(os.Stdout, data)
	case *api.HmeEmail:
		output.PrintDetail(os.Stdout, data)
	default:
		return output.PrintJSON(os.Stdout, v)
	}
	return nil
}

func ExactRefArg(use, example string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("<ref> required — an address label, email, or ID\n\n  Usage: %s\n  Example: %s", use, example)
		}
		if len(args) > 1 {
			return fmt.Errorf("expected 1 argument, got %d\n\n  Usage: %s", len(args), use)
		}
		return nil
	}
}

func CheckErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, Explain(err))
		os.Exit(ExitCode(err))
	}
}
