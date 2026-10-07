package cmdutil

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/pkg/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ErrCancelled is the user declining a prompt. Not a failure: it
// prints "Cancelled." and exits 0, like the delete confirmation.
var ErrCancelled = errors.New("cancelled")

// pickShown caps the picker. Past it, a ref is faster than scrolling.
const pickShown = 9

// isTerminal is a seam so tests can stand in for a real terminal.
var isTerminal = term.IsTerminal

// Pick describes what an omitted <ref> offers.
type Pick struct {
	// Verb completes "Which address to ...?".
	Verb string
	// Want keeps the addresses this command can act on; nil keeps all.
	Want func(api.HmeEmail) bool
	// None is the error when nothing qualifies.
	None string
}

// RefOrPick is the Args contract for commands that act on one
// address: exactly one <ref>, or none when the user is at a terminal
// and can be asked. Pipes, scripts, and --json/--jq never get a
// prompt; they get the usage error, exactly as before.
func RefOrPick(use, example string) cobra.PositionalArgs {
	exact := ExactRefArg(use, example)
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && CanPrompt(cmd) {
			return nil
		}
		return exact(cmd, args)
	}
}

// CanPrompt reports whether a human can answer a question: stdin and
// stderr are terminals, no machine-readable output was requested, and
// IHME_NO_PROMPT is unset. The variable is for harnesses that drive
// ihme inside a pseudo-terminal (tmux, expect), where a TTY is not a
// person.
func CanPrompt(cmd *cobra.Command) bool {
	if os.Getenv("IHME_NO_PROMPT") != "" {
		return false
	}
	if j, _ := cmd.Flags().GetBool("json"); j {
		return false
	}
	if q, _ := cmd.Flags().GetString("jq"); q != "" {
		return false
	}
	return isTerminal(int(os.Stdin.Fd())) && isTerminal(int(os.Stderr.Fd()))
}

// RefFromArgs returns args[0], or, when it was omitted, asks which
// address to act on and returns its anonymousId.
func RefFromArgs(args []string, client *api.Client, p Pick) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	result, err := client.ListHme()
	if err != nil {
		return "", err
	}
	hme, err := PickAddress(os.Stdin, os.Stderr, result.HmeEmails, p, time.Now())
	if err != nil {
		return "", err
	}
	return hme.AnonymousID, nil
}

// PickAddress offers the newest addresses that fit p, newest first,
// so a bare Enter means the address created most recently: usually
// the one the user just made and is now acting on.
func PickAddress(in io.Reader, out io.Writer, emails []api.HmeEmail, p Pick, now time.Time) (*api.HmeEmail, error) {
	var fit []api.HmeEmail
	for _, e := range emails {
		if p.Want == nil || p.Want(e) {
			fit = append(fit, e)
		}
	}
	if len(fit) == 0 {
		return nil, errors.New(p.None)
	}
	sort.SliceStable(fit, func(i, j int) bool { return fit[i].CreateTimestamp > fit[j].CreateTimestamp })
	shown := fit[:min(len(fit), pickShown)]

	// State is worth a column only when the list mixes it.
	mixed := false
	for _, e := range shown {
		mixed = mixed || e.IsActive != shown[0].IsActive
	}
	fmt.Fprintf(out, "Which address to %s? Newest first:\n\n", p.Verb)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for i, e := range shown {
		label := e.Label
		if label == "" {
			label = "-"
		}
		row := fmt.Sprintf("  %d)\t%s\t%s\t%s", i+1, e.Hme, label, output.Ago(time.UnixMilli(e.CreateTimestamp), now))
		if mixed && !e.IsActive {
			row += "\tinactive"
		}
		fmt.Fprintln(tw, row)
	}
	tw.Flush()
	if more := len(fit) - len(shown); more > 0 {
		fmt.Fprintf(out, "  ... %d older; pass a label, address, or ID to reach them\n", more)
	}

	if len(shown) == 1 {
		fmt.Fprintf(out, "\nEnter for 1, or q to cancel: ")
	} else {
		fmt.Fprintf(out, "\nEnter for 1, a number 1-%d, or q to cancel: ", len(shown))
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		// EOF or a closed terminal is not consent to anything.
		fmt.Fprintln(out)
		return nil, ErrCancelled
	}
	switch choice := strings.ToLower(strings.TrimSpace(line)); choice {
	case "":
		return &shown[0], nil
	case "q", "quit", "c", "cancel", "n", "no":
		return nil, ErrCancelled
	default:
		n, err := strconv.Atoi(choice)
		if err != nil || n < 1 || n > len(shown) {
			return nil, fmt.Errorf("no choice %q; run it again and pick 1-%d, or pass a label, address, or ID", choice, len(shown))
		}
		return &shown[n-1], nil
	}
}
