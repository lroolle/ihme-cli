// Package share prints the key and link that open one address's mail
// in ihme-inbox, the web inbox that used to be `ihme serve`.
package share

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lroolle/ihme-cli/internal/clip"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/internal/inboxkey"
	"github.com/lroolle/ihme-cli/pkg/resolver"
	"github.com/spf13/cobra"
)

const inboxURL = "https://github.com/lroolle/ihme-inbox"

func NewCmdShare() *cobra.Command {
	var baseURL string
	var copyLink bool

	cmd := &cobra.Command{
		Use:   "share [ref]",
		Short: "Let someone read one address's mail in the web inbox",
		Long: `Print the key and the link that open one Hide My Email address's mail
in ihme-inbox (` + inboxURL + `). Whoever has
them reads the mail that address received, and nothing else: not your
other addresses, not your account.

Nothing is stored and the server is not contacted. A key is computed
from the address and a secret this machine shares with the server
('ihme share secret' creates it and prints the line the server needs).
Running this again prints another key; the earlier ones keep working
until the address is revoked on the server: ihme-inbox revoke <address>.
Without <ref> at a terminal, asks which address, newest first.

Save your server once with 'ihme share config --url https://mail.example.com'.
Both share and new --share then use it. --url and IHME_INBOX_URL override it.
Add --copy to copy the link; if the clipboard is unavailable, the link is
still printed. Clipboard status goes to stderr, including with --json.

JSON output (--json):
  {"address":"...","label":"...","key":"...","link":"...","mintedAt":"...",
   "hints":{"revoke":"ihme-inbox revoke <address>","serve":"ihme-inbox serve"}}`,
		Example: `  ihme share netflix
  ihme share netflix --copy
  ihme share config --url https://mail.example.com
  ihme share netflix --url https://mail.example.com
  ihme new netflix --share
  ihme share secret`,
		Args: cmdutil.RefOrPick("ihme share <ref>", "ihme share netflix"),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Before any iCloud call: a machine with no secret cannot
			// share, and should hear that first.
			if _, _, _, err := inboxkey.LoadSecret(); err != nil {
				return err
			}
			resolvedURL, err := inboxkey.BaseURL(baseURL)
			if err != nil {
				return err
			}
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}
			ref, err := cmdutil.RefFromArgs(args, client, cmdutil.Pick{
				Verb: "share",
				None: "no addresses yet — create one with: ihme new <label>",
			})
			if err != nil {
				return err
			}
			result, err := client.ListHme()
			if err != nil {
				return err
			}
			hme, err := resolver.Resolve(ref, result.HmeEmails)
			if err != nil {
				return err
			}
			grant, err := inboxkey.Issue(hme.Hme, resolvedURL, time.Now())
			if err != nil {
				return err
			}
			if copyLink {
				if err := clip.Copy(grant.Link); err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "Clipboard unavailable; use the printed link.")
				} else {
					fmt.Fprintln(cmd.ErrOrStderr(), "Inbox link copied.")
				}
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			jqFlag, _ := cmd.Flags().GetString("jq")
			if jsonFlag || jqFlag != "" {
				return cmdutil.OutputResult(cmd, map[string]any{
					"address":  hme.Hme,
					"label":    hme.Label,
					"key":      grant.Key,
					"link":     grant.Link,
					"mintedAt": grant.MintedAt,
					"hints": map[string]string{
						"revoke": "ihme-inbox revoke " + hme.Hme,
						"serve":  "ihme-inbox serve",
					},
				})
			}

			name := hme.Hme
			if hme.Label != "" {
				name += " (" + hme.Label + ")"
			}
			fmt.Printf("Shared %s\n", name)
			fmt.Print(grant.Text(hme.Hme))
			if !hme.IsActive {
				fmt.Println("This address is inactive: it receives no new mail.")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&baseURL, "url", "", "Public URL of ihme-inbox for the printed link (or set IHME_INBOX_URL)")
	cmd.Flags().BoolVar(&copyLink, "copy", false, "Copy the inbox link to the clipboard")
	cmd.AddCommand(newCmdConfig())
	cmd.AddCommand(newCmdSecret())
	cmd.AddCommand(moved("list", "ihme no longer keeps a list of shared addresses\n\n"+
		"  A key is computed from the address, so nothing is recorded.\n"+
		"  Print a key again:                    ihme share <ref>\n"+
		"  See what was revoked, on the server:  ihme-inbox revoke --list"))
	cmd.AddCommand(moved("revoke", "revoking moved to the server\n\n"+
		"  On the machine that runs ihme-inbox:  ihme-inbox revoke <address>\n"+
		"  It stops every key made so far for that address, at once.\n"+
		"  For a new key, make it there too:     ihme-inbox key <address>\n"+
		"  (a key made here in the seconds after a revoke may not open)"))
	return cmd
}

func newCmdConfig() *cobra.Command {
	var baseURL string
	cmd := &cobra.Command{
		Use:     "config",
		Short:   "Show or save the default inbox URL",
		Long:    "Save the server URL for share and new --share, without changing the secret.\nFlags and IHME_INBOX_URL (or IHME_SERVE_URL) override the saved URL.\n\nJSON output (--json): {\"url\":\"...\",\"hints\":{\"share\":\"ihme share <ref> --copy\"}}",
		Example: "  ihme share config --url https://mail.example.com\n  ihme share config --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("url") {
				if _, err := inboxkey.SaveURL(baseURL); err != nil {
					return err
				}
			}
			effective, err := inboxkey.BaseURL("")
			if err != nil {
				return err
			}
			jsonFlag, _ := cmd.Flags().GetBool("json")
			jqFlag, _ := cmd.Flags().GetString("jq")
			if !jsonFlag && jqFlag == "" {
				fmt.Printf("Inbox URL: %s\n", effective)
				if cmd.Flags().Changed("url") {
					fmt.Println("Saved for share and new --share. Environment variables override this default.")
				}
				return nil
			}
			return cmdutil.OutputResult(cmd, map[string]any{
				"url":   effective,
				"hints": map[string]string{"share": "ihme share <ref> --copy"},
			})
		},
	}
	cmd.Flags().StringVar(&baseURL, "url", "", "Save the default public URL of ihme-inbox")
	return cmd
}

func newCmdSecret() *cobra.Command {
	return &cobra.Command{
		Use:   "secret",
		Short: "Print the secret your ihme-inbox server needs",
		Long: `Print the secret that keys are made from, as the line an ihme-inbox
server's environment needs. The first run on a machine creates it.

The secret is $IHME_INBOX_SECRET if set, else the file 'secret' in
$IHME_INBOX_DIR (default ~/.config/ihme-inbox), the same file
ihme-inbox reads. Whoever has it can make a key for any address: keep
it like a password. To replace it, delete the file; every key ever
handed out stops working.

JSON output (--json):
  {"secret":"...","from":"...","created":false}`,
		Example: `  ihme share secret
  ihme share secret | ssh server 'cat >> ihme-inbox/deploy/.env'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			text, _, from, err := inboxkey.LoadSecret()
			created := errors.Is(err, inboxkey.ErrNoSecret)
			if created {
				text, from, err = inboxkey.CreateSecret()
			}
			if err != nil {
				return err
			}
			jsonFlag, _ := cmd.Flags().GetBool("json")
			jqFlag, _ := cmd.Flags().GetString("jq")
			if jsonFlag || jqFlag != "" {
				return cmdutil.OutputResult(cmd, map[string]any{"secret": text, "from": from, "created": created})
			}
			if created {
				fmt.Fprintf(os.Stderr, "Created a new secret in %s.\n", from)
			} else {
				fmt.Fprintf(os.Stderr, "Secret from %s.\n", from)
			}
			fmt.Fprintln(os.Stderr, "Your ihme-inbox server needs this line in its environment. It makes keys for any address: keep it like a password.")
			fmt.Printf("IHME_INBOX_SECRET=%s\n", text)
			return nil
		},
	}
}

// NewCmdServe answers `ihme serve`, which v0.7.0 had: the web inbox is
// its own program now.
func NewCmdServe() *cobra.Command {
	return moved("serve", "the web inbox moved to its own program, ihme-inbox\n\n"+
		"  Get it:  "+inboxURL+"\n"+
		"  Run it:  ihme-inbox serve\n"+
		"  Keys from ihme 0.7.0 no longer open anything. Share again: ihme share <ref>")
}

// moved keeps a v0.7.0 command name alive long enough to say where it
// went, whatever flags it is called with.
func moved(name, where string) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Long:               where, // `ihme help serve` says it too
		Hidden:             true,
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE:               func(cmd *cobra.Command, args []string) error { return errors.New(where) },
	}
}
