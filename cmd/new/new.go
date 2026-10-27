package new

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/agent"
	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/internal/inboxkey"
	"github.com/lroolle/ihme-cli/internal/memory"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func NewCmdNew() *cobra.Command {
	var (
		note    string
		tagList []string
		yes     bool
		address string
		count   int
		agentic bool
		grant   string
		effort  string
		shared  bool
	)

	cmd := &cobra.Command{
		Use:   "new <label>",
		Short: "Generate and reserve a new Hide My Email address",
		Long: `Generate Hide My Email address candidates and reserve one.

Matches the iCloud web flow: generate first, pick, then reserve.

At a terminal, copies the reserved address to the system clipboard.
With --share, copies the inbox link instead. Use --no-copy to opt out,
or --copy to request copying with piped or JSON output. Clipboard failure
never fails the reservation; the address and link are still printed.

JSON output without --yes (candidates, no reservation):
  {"candidates":["a@icloud.com","b@icloud.com",...],"label":"...","hint":"ihme new <label> --address <addr>"}

JSON output with --yes or --address (reserved):
  {"anonymousId":"...","label":"...","hme":"a@icloud.com","isActive":true,...}

--share also prints the key and link that open the new address's mail
in ihme-inbox (see 'ihme share --help'); in JSON, as one more field:
  {...,"share":{"key":"...","link":"...","mintedAt":"..."}}`,
		Example: `  ihme new github.com
  ihme new github.com --json
  ihme new github.com --address pole-toils-3x@icloud.com
  ihme new github.com -y --json
  ihme new netflix --share
  ihme new github.com --tag dev --note "main"`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("label required\n\n  Usage: ihme new <label>\n  Example: ihme new github.com")
			}
			if len(args) > 1 {
				return fmt.Errorf("too many arguments\n\n  Usage: ihme new <label>\n  Got: %s", strings.Join(args, " "))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			label := args[0]
			jsonFlag, _ := cmd.Flags().GetBool("json")
			// --jq is machine output too: without this, "--jq .candidates"
			// in a pipe took the non-TTY path and reserved an address.
			if jq, _ := cmd.Flags().GetString("jq"); jq != "" {
				jsonFlag = true
			}

			if shared {
				// Checked before anything is reserved: an address made
				// and then not shared is a surprise to clean up.
				if jsonFlag && !yes && address == "" && !agentic {
					return fmt.Errorf("--share needs a reservation, and --json alone only lists candidates\n\n  Add --yes, or --address <candidate>")
				}
				if _, _, _, err := inboxkey.LoadSecret(); err != nil {
					return err
				}
				if _, err := inboxkey.BaseURL(""); err != nil {
					return err
				}
			}

			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}
			svc := app.New(client)

			// --agent: run the embedded agent through the SKILL.md
			// procedure instead of the imperative flow.
			if agentic {
				if grant != string(agent.GrantAsk) && grant != string(agent.GrantAuto) {
					return fmt.Errorf("invalid --grant %q — use ask or auto", grant)
				}
				res, err := agent.RunNew(cmd.Context(), svc, client.Session().AppleID, agent.Options{
					Label: label, Note: note, Grant: agent.GrantMode(grant), Effort: effort, JSON: jsonFlag, NoClipboard: true,
				})
				// A run can reserve and then fail (the model's closing
				// turn times out). The address exists either way, so it
				// is reported, with its key, before the error is.
				if err != nil && (res == nil || res.Reserved == nil) {
					return err
				}
				runErr := err
				if res.Reserved == nil {
					if jsonFlag {
						return cmdutil.OutputResult(cmd, res)
					}
					fmt.Println("No address reserved.")
					return nil
				}
				g, err := grantFor(res.Reserved, shared)
				if err != nil {
					return err
				}
				copyReserved(cmd, res.Reserved, g)
				if jsonFlag {
					var out any = res
					if g != nil {
						out = struct {
							*agent.Result
							Share *inboxkey.Grant `json:"share"`
						}{res, g}
					}
					if err := cmdutil.OutputResult(cmd, out); err != nil {
						return err
					}
					return runErr
				}
				printReserved(res.Reserved, g)
				return runErr
			}

			// --address: reserve a specific address (agent step 2)
			if address != "" {
				return reserve(svc, address, label, tagList, note, jsonFlag, shared, cmd)
			}

			// --yes: generate one, reserve, done (script mode)
			if yes {
				candidates, err := svc.Generate(1)
				if err != nil {
					return err
				}
				return reserve(svc, candidates[0], label, tagList, note, jsonFlag, shared, cmd)
			}

			candidates, err := svc.Generate(count)
			if err != nil {
				return err
			}

			// --json without --yes: return candidates only (agent step 1)
			if jsonFlag {
				out := map[string]any{
					"candidates": candidates,
					"label":      label,
					"hint":       fmt.Sprintf("ihme new %s --address <address> --json", label),
				}
				// The external agent ranks from this payload alone: the
				// user's standing preferences travel with the pool, the
				// same as the embedded generate_candidates result.
				if prefs := memory.Open().Bullets(memory.PreferencesPage); len(prefs) > 0 {
					out["preferences"] = prefs
				}
				return cmdutil.OutputResult(cmd, out)
			}

			// non-TTY: take first
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return reserve(svc, candidates[0], label, tagList, note, false, shared, cmd)
			}

			// interactive: pick from list
			return interactive(svc, candidates, label, tagList, note, shared, cmd)
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "Note for the address")
	cmd.Flags().StringSliceVar(&tagList, "tag", nil, "Tags (repeatable)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Reserve the first candidate immediately")
	cmd.Flags().StringVar(&address, "address", "", "Reserve a specific generated address")
	cmd.Flags().IntVarP(&count, "count", "n", 3, "Number of candidates to generate")
	cmd.Flags().BoolVar(&agentic, "agent", false, "Run the embedded agent: search, generate, judge, reserve per SKILL.md")
	cmd.Flags().StringVar(&grant, "grant", "ask", "Agent consent for actions beyond this run's scope: ask or auto")
	cmd.Flags().StringVar(&effort, "effort", "", "Agent reasoning effort: minimal, low, medium, high (some models also: xhigh, max)")
	cmd.Flags().BoolVar(&shared, "share", false, "Also print the key and link that open the new address in ihme-inbox")
	cmd.Flags().Bool("copy", false, "Copy the address (or inbox link with --share), including in scripts")
	cmd.Flags().Bool("no-copy", false, "Do not change the clipboard")
	cmd.MarkFlagsMutuallyExclusive("copy", "no-copy")
	return cmd
}

func reserve(svc *app.Service, hme, label string, tagList []string, note string, jsonFlag, shared bool, cmd *cobra.Command) error {
	reserved, err := svc.Reserve(hme, label, tagList, note)
	if err != nil {
		return err
	}
	agent.JournalReservation(reserved)
	g, err := grantFor(reserved, shared)
	if err != nil {
		return err
	}
	copyReserved(cmd, reserved, g)
	if jsonFlag {
		if g != nil {
			return cmdutil.OutputResult(cmd, struct {
				*api.HmeEmail
				Share *inboxkey.Grant `json:"share"`
			}{reserved, g})
		}
		return cmdutil.OutputResult(cmd, reserved)
	}
	printReserved(reserved, g)
	return nil
}

// grantFor mints the new address's key when --share asked for one. A
// failure here comes after the reservation, so the error says the
// address exists and how to get its key.
func grantFor(reserved *api.HmeEmail, shared bool) (*inboxkey.Grant, error) {
	if !shared {
		return nil, nil
	}
	g, err := inboxkey.Issue(reserved.Hme, "", time.Now())
	if err != nil {
		return nil, fmt.Errorf("reserved %s (label: %s), but could not make its key: %w\n\n  The address exists. Its key: ihme share %s",
			reserved.Hme, reserved.Label, err, reserved.Hme)
	}
	return g, nil
}

func printReserved(reserved *api.HmeEmail, g *inboxkey.Grant) {
	fmt.Printf("Reserved: %s (label: %s)\n", reserved.Hme, reserved.Label)
	if g != nil {
		fmt.Print(g.Text(reserved.Hme))
	}
}

func interactive(svc *app.Service, candidates []string, label string, tagList []string, note string, shared bool, cmd *cobra.Command) error {
	fmt.Println()
	for i, c := range candidates {
		fmt.Printf("  [%d] %s\n", i+1, c)
	}
	fmt.Printf("\nSelect [1-%d] or [c]ancel: ", len(candidates))

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	choice := strings.TrimSpace(strings.ToLower(line))
	if choice == "c" || choice == "cancel" {
		fmt.Println("Cancelled.")
		return nil
	}

	var idx int
	if _, err := fmt.Sscan(choice, &idx); err != nil || idx < 1 || idx > len(candidates) {
		return fmt.Errorf("invalid choice — enter 1-%d or c", len(candidates))
	}

	return reserve(svc, candidates[idx-1], label, tagList, note, false, shared, cmd)
}
