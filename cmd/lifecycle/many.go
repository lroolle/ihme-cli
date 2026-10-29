package lifecycle

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/resolver"
	"github.com/spf13/cobra"
)

// step is one lifecycle command applied to many addresses.
type step struct {
	// use starts the retry line, with the flags the user chose:
	// "ihme delete --force --yes".
	use string
	// done is the progress word, "Deleted"; lowercased it is the
	// JSON status.
	done string
	// skip says why an address needs nothing ("already inactive"),
	// "" to act on it. nil acts on every address.
	skip func(api.HmeEmail) string
	act  func(api.HmeEmail) error
}

// outcome is one address in the --json results of a batch.
type outcome struct {
	Status string `json:"status"` // the step's done word, skipped, failed, not_attempted
	Hme    string `json:"hme"`
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

// resolveAll lists once and resolves every ref before anything
// changes.
func resolveAll(client *api.Client, refs []string) ([]api.HmeEmail, error) {
	result, err := client.ListHme()
	if err != nil {
		return nil, err
	}
	return resolver.ResolveAll(refs, result.HmeEmails)
}

// many runs s over emails, printing a line per address, or the
// results as JSON with --json/--jq.
func many(cmd *cobra.Command, emails []api.HmeEmail, s step) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	jqExpr, _ := cmd.Flags().GetString("jq")
	machine := jsonFlag || jqExpr != ""

	results, err := runMany(os.Stdout, os.Stderr, emails, s, machine)
	if machine {
		out := map[string]any{"results": results}
		if retry := retryCommand(s, results); retry != "" {
			out["hints"] = map[string]string{"retry": retry}
		}
		// Changes already made outrank a broken --jq: keep the batch's
		// own error, and with it the exit code.
		if oerr := cmdutil.OutputResult(cmd, out); oerr != nil && err == nil {
			return oerr
		}
	}
	return err
}

// retryCommand is s again for every address it did not finish, ""
// when it finished them all.
func retryCommand(s step, results []outcome) string {
	var left []string
	for _, r := range results {
		if r.Status == "failed" || r.Status == "not_attempted" {
			left = append(left, r.Hme)
		}
	}
	if len(left) == 0 {
		return ""
	}
	return s.use + " " + strings.Join(left, " ")
}

// runMany applies s to each address in order and stops at the first
// failure. An error from Apple is rarely about one address: it is the
// session, the network, or rate pressure, and it would repeat for
// every address left. A rejected session would even retry the
// sign-in each time, which is how accounts get rate limited. What
// was not done comes back as one command that retries exactly those.
func runMany(out, errOut io.Writer, emails []api.HmeEmail, s step, quiet bool) ([]outcome, error) {
	results := make([]outcome, 0, len(emails))
	var stop error
	var stoppedAt string
	notDone := 0
	for _, e := range emails {
		r := outcome{Hme: e.Hme, ID: e.AnonymousID}
		reason := ""
		if s.skip != nil {
			reason = s.skip(e)
		}
		switch {
		case reason != "":
			r.Status, r.Reason = "skipped", reason
			if !quiet {
				fmt.Fprintf(out, "%s is %s\n", e.Hme, reason)
			}
		case stop != nil:
			r.Status = "not_attempted"
			notDone++
		default:
			if err := s.act(e); err != nil {
				r.Status, r.Error = "failed", err.Error()
				notDone++
				stop, stoppedAt = err, e.Hme
				break
			}
			r.Status = strings.ToLower(s.done)
			if !quiet {
				fmt.Fprintf(out, "%s %s\n", s.done, e.Hme)
			}
		}
		results = append(results, r)
	}

	if stop == nil {
		return results, nil
	}
	fmt.Fprintf(errOut, "Stopped at %s: %d of %d not %s. Once the error below is fixed, retry them with:\n  %s\n",
		stoppedAt, notDone, len(emails), strings.ToLower(s.done), retryCommand(s, results))
	return results, stop
}

// confirmDelete asks once for a batch delete, listing every address.
// Everything that would refuse is checked first, so a refusal
// deletes nothing.
func confirmDelete(in io.Reader, errOut io.Writer, emails []api.HmeEmail, force, yes, canPrompt bool) error {
	var active []string
	for _, e := range emails {
		if e.IsActive {
			active = append(active, e.Hme)
		}
	}
	if len(active) > 0 && !force {
		return fmt.Errorf("nothing deleted: %d of %d still active — deactivate them first, or pass --force to do both\n  %s",
			len(active), len(emails), strings.Join(active, "\n  "))
	}
	if yes {
		return nil
	}
	if !canPrompt {
		return fmt.Errorf("refusing to delete %d addresses without confirmation — pass --yes", len(emails))
	}

	fmt.Fprintf(errOut, "These %d addresses will be deleted. This cannot be undone.\n\n", len(emails))
	tw := tabwriter.NewWriter(errOut, 0, 0, 2, ' ', 0)
	for _, e := range emails {
		label := e.Label
		if label == "" {
			label = "-"
		}
		row := fmt.Sprintf("  %s\t%s\t%s", e.Hme, label, e.CreatedAt().Format("2006-01-02"))
		if e.IsActive {
			row += "\tactive"
		}
		fmt.Fprintln(tw, row)
	}
	tw.Flush()
	if len(active) > 0 {
		fmt.Fprintf(errOut, "\n%d of them are active; --force deactivates them first.", len(active))
	}
	fmt.Fprintf(errOut, "\nDelete them? [y/N] ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(strings.ToLower(line)) != "y" {
		return cmdutil.ErrCancelled
	}
	return nil
}
