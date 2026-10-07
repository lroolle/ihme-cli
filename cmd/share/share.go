// Package share mints and revokes the per-address access keys that
// `ihme serve` checks. It is the only admin surface the web view has.
package share

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/internal/shares"
	"github.com/lroolle/ihme-cli/pkg/output"
	"github.com/lroolle/ihme-cli/pkg/resolver"
	"github.com/spf13/cobra"
)

const defaultURL = "http://127.0.0.1:8025"

func NewCmdShare() *cobra.Command {
	var baseURL string

	cmd := &cobra.Command{
		Use:   "share [ref]",
		Short: "Let someone read one address's mail in the web inbox",
		Long: `Mint an access key for one Hide My Email address. Whoever holds the
key can read the mail that address received in 'ihme serve', and
nothing else: not your other addresses, not your account.

The key is printed once; ihme keeps only its hash. Sharing the same
address again rotates its key, and the old one stops working.
Without <ref> at a terminal, asks which address, newest first.

JSON output (--json):
  {"address":"...","label":"...","key":"...","link":"...","rotated":false,
   "hints":{"revoke":"ihme share revoke <address>","serve":"ihme serve"}}`,
		Example: `  ihme share netflix
  ihme share netflix --url https://mail.example.com
  ihme share list
  ihme share revoke netflix`,
		Args: cmdutil.RefOrPick("ihme share <ref>", "ihme share netflix"),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			path := sharesPath()
			key, rotated, err := shares.Grant(path, hme.Hme, hme.AnonymousID, hme.Label, time.Now())
			if err != nil {
				return err
			}
			link := linkFor(baseURL, key)

			jsonFlag, _ := cmd.Flags().GetBool("json")
			jqFlag, _ := cmd.Flags().GetString("jq")
			if jsonFlag || jqFlag != "" {
				return cmdutil.OutputResult(cmd, map[string]any{
					"address": hme.Hme,
					"label":   hme.Label,
					"key":     key,
					"link":    link,
					"rotated": rotated,
					"hints": map[string]string{
						"revoke": "ihme share revoke " + hme.Hme,
						"serve":  "ihme serve",
					},
				})
			}

			name := hme.Hme
			if hme.Label != "" {
				name += " (" + hme.Label + ")"
			}
			fmt.Printf("Shared %s\n\n", name)
			fmt.Printf("  Key   %s\n", key)
			fmt.Printf("  Link  %s\n\n", link)
			fmt.Println("The key is shown once; ihme keeps only its hash. Whoever has it")
			fmt.Println("reads this address's mail in 'ihme serve', and nothing else.")
			if rotated {
				fmt.Println("The previous key for this address no longer works.")
			}
			if !hme.IsActive {
				fmt.Println("This address is inactive: it receives no new mail.")
			}
			fmt.Printf("Revoke: ihme share revoke %s\n", hme.Hme)
			return nil
		},
	}

	cmd.Flags().StringVar(&baseURL, "url", "", "Public URL of 'ihme serve' for the printed link (or set IHME_SERVE_URL)")
	cmd.AddCommand(newCmdList())
	cmd.AddCommand(newCmdRevoke())
	return cmd
}

func sharesPath() string {
	return shares.DefaultPath(api.DefaultSessionPath())
}

func linkFor(baseURL, key string) string {
	if baseURL == "" {
		baseURL = os.Getenv("IHME_SERVE_URL")
	}
	if baseURL == "" {
		baseURL = defaultURL
	}
	return strings.TrimRight(baseURL, "/") + "/k/" + key
}

type listed struct {
	Address     string    `json:"address"`
	Label       string    `json:"label"`
	AnonymousID string    `json:"anonymousId"`
	CreatedAt   time.Time `json:"createdAt"`
}

func newCmdList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List addresses that have a live access key",
		Long: `List addresses that have a live access key. Reads the local shares
file only; no iCloud call.

JSON output (--json):
  [{"address":"...","label":"...","anonymousId":"...","createdAt":"..."}]`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := shares.Load(sharesPath())
			if err != nil {
				return err
			}
			out := make([]listed, 0, len(list))
			for _, s := range list {
				out = append(out, listed{Address: s.Address, Label: s.Label, AnonymousID: s.AnonymousID, CreatedAt: s.CreatedAt})
			}
			jsonFlag, _ := cmd.Flags().GetBool("json")
			jqFlag, _ := cmd.Flags().GetString("jq")
			if jsonFlag || jqFlag != "" {
				return cmdutil.OutputResult(cmd, out)
			}
			if len(out) == 0 {
				fmt.Fprintln(os.Stderr, "No addresses shared. Share one with: ihme share <ref>")
				return nil
			}
			now := time.Now()
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ADDRESS\tLABEL\tSHARED")
			for _, s := range out {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Address, dash(s.Label), output.Ago(s.CreatedAt, now))
			}
			return tw.Flush()
		},
	}
}

func newCmdRevoke() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "revoke [ref]",
		Short: "Stop an access key from working",
		Long: `Stop an address's access key from working. A running 'ihme serve'
refuses it on the next request. <ref> is the address, its label, or
its ID; this reads the local shares file only, so it works offline.
Without <ref> at a terminal, asks which share. --all revokes every key.

JSON output (--json):
  {"revoked":["address", ...]}`,
		Example: `  ihme share revoke netflix
  ihme share revoke brisk.heron_0q@icloud.com
  ihme share revoke --all`,
		Args: func(cmd *cobra.Command, args []string) error {
			if all {
				return cobra.NoArgs(cmd, args)
			}
			return cmdutil.RefOrPick("ihme share revoke <ref>", "ihme share revoke netflix")(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			path := sharesPath()
			list, err := shares.Load(path)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				return fmt.Errorf("no addresses are shared")
			}

			var match func(shares.Share) bool
			switch {
			case all:
				match = func(shares.Share) bool { return true }
			case len(args) == 1:
				target, err := findShare(list, args[0])
				if err != nil {
					return err
				}
				match = func(s shares.Share) bool { return s.Address == target }
			default:
				picked, err := cmdutil.PickAddress(os.Stdin, os.Stderr, asEmails(list), cmdutil.Pick{
					Verb: "stop sharing",
					None: "no addresses are shared",
				}, time.Now())
				if err != nil {
					return err
				}
				match = func(s shares.Share) bool { return s.Address == picked.Hme }
			}

			removed, err := shares.Revoke(path, match)
			if err != nil {
				return err
			}
			addresses := make([]string, 0, len(removed))
			for _, s := range removed {
				addresses = append(addresses, s.Address)
			}
			jsonFlag, _ := cmd.Flags().GetBool("json")
			jqFlag, _ := cmd.Flags().GetString("jq")
			if jsonFlag || jqFlag != "" {
				return cmdutil.OutputResult(cmd, map[string]any{"revoked": addresses})
			}
			for _, a := range addresses {
				fmt.Printf("Revoked %s\n", a)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Revoke every access key")
	return cmd
}

// findShare matches ref against the local shares: exact address,
// exact label, ID prefix, then a unique label substring. The same
// order the iCloud resolver uses, without the network.
func findShare(list []shares.Share, ref string) (string, error) {
	emails := asEmails(list)
	hme, err := resolver.Resolve(ref, emails)
	if err != nil {
		return "", fmt.Errorf("%w among shared addresses — see: ihme share list", err)
	}
	return hme.Hme, nil
}

// asEmails lets the shared addresses ride the same resolver and
// picker as live ones.
func asEmails(list []shares.Share) []api.HmeEmail {
	out := make([]api.HmeEmail, 0, len(list))
	for _, s := range list {
		out = append(out, api.HmeEmail{
			Hme: s.Address, Label: s.Label, AnonymousID: s.AnonymousID,
			IsActive: true, CreateTimestamp: s.CreatedAt.UnixMilli(),
		})
	}
	return out
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
