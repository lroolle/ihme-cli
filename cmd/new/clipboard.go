package new

import (
	"fmt"
	"os"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/clip"
	"github.com/lroolle/ihme-cli/internal/inboxkey"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var clipboardTerminal = term.IsTerminal
var copyText = clip.Copy

func copyReserved(cmd *cobra.Command, reserved *api.HmeEmail, grant *inboxkey.Grant) {
	noCopy, _ := cmd.Flags().GetBool("no-copy")
	if noCopy {
		return
	}
	requested, _ := cmd.Flags().GetBool("copy")
	json, _ := cmd.Flags().GetBool("json")
	jq, _ := cmd.Flags().GetString("jq")
	out, terminal := cmd.OutOrStdout().(*os.File)
	if !requested && (json || jq != "" || os.Getenv("IHME_NO_PROMPT") != "" || !terminal || !clipboardTerminal(int(out.Fd()))) {
		return
	}
	text, what := reserved.Hme, "Address"
	if grant != nil {
		text, what = grant.Link, "Inbox link"
	}
	if err := copyText(text); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Clipboard unavailable; use the printed address or link.")
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s copied.\n", what)
}
