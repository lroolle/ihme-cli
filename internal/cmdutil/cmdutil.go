package cmdutil

import (
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

func GetClient(cmd *cobra.Command) (*api.Client, error) {
	sessPath := api.DefaultSessionPath()
	sess, err := api.LoadSession(sessPath)
	if err != nil {
		return nil, fmt.Errorf("loading session: %w", err)
	}
	if sess == nil {
		return nil, fmt.Errorf("not logged in — run 'ihme auth login' first")
	}

	client, err := api.NewClientWithSession(sess)
	if err != nil {
		return nil, err
	}
	client.Verbose, _ = cmd.Flags().GetBool("verbose")

	// Recently confirmed sessions skip the validate round trip.
	if time.Since(sess.ValidatedAt) < validateTTL {
		return client, nil
	}

	if err := client.ResumeSession(); err != nil {
		if api.IsTransient(err) {
			return nil, fmt.Errorf("iCloud is temporarily unreachable — your session is probably still valid, try again shortly: %w", err)
		}
		return nil, fmt.Errorf("session expired — run 'ihme auth login' to re-authenticate: %w", err)
	}

	// Save updated session (cookies + validation timestamp refreshed)
	api.SaveSession(sessPath, client.Session())

	return client, nil
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
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
