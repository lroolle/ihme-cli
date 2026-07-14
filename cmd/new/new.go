package new

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/lroolle/ihme-cli/internal/agent"
	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
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
	)

	cmd := &cobra.Command{
		Use:   "new <label>",
		Short: "Generate and reserve a new Hide My Email address",
		Long: `Generate Hide My Email address candidates and reserve one.

Matches the iCloud web flow: generate first, pick, then reserve.

JSON output without --yes (candidates, no reservation):
  {"candidates":["a@icloud.com","b@icloud.com",...],"label":"...","hint":"ihme new <label> --address <addr>"}

JSON output with --yes or --address (reserved):
  {"anonymousId":"...","label":"...","hme":"a@icloud.com","isActive":true,...}`,
		Example: `  ihme new github.com
  ihme new github.com --json
  ihme new github.com --address pole-toils-3x@icloud.com
  ihme new github.com -y --json
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
					Label: label, Note: note, Grant: agent.GrantMode(grant), JSON: jsonFlag,
				})
				if err != nil {
					return err
				}
				if jsonFlag {
					return cmdutil.OutputResult(cmd, res)
				}
				if res.Reserved != nil {
					fmt.Printf("Reserved: %s (label: %s)\n", res.Reserved.Hme, res.Reserved.Label)
				} else {
					fmt.Println("No address reserved.")
				}
				return nil
			}

			// --address: reserve a specific address (agent step 2)
			if address != "" {
				return reserve(svc, address, label, tagList, note, jsonFlag, cmd)
			}

			// --yes: generate one, reserve, done (script mode)
			if yes {
				candidates, err := svc.Generate(1)
				if err != nil {
					return err
				}
				return reserve(svc, candidates[0], label, tagList, note, jsonFlag, cmd)
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
				return cmdutil.OutputResult(cmd, out)
			}

			// non-TTY: take first
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return reserve(svc, candidates[0], label, tagList, note, false, cmd)
			}

			// interactive: pick from list
			return interactive(svc, candidates, label, tagList, note, cmd)
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "Note for the address")
	cmd.Flags().StringSliceVar(&tagList, "tag", nil, "Tags (repeatable)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Reserve the first candidate immediately")
	cmd.Flags().StringVar(&address, "address", "", "Reserve a specific generated address")
	cmd.Flags().IntVarP(&count, "count", "n", 3, "Number of candidates to generate")
	cmd.Flags().BoolVar(&agentic, "agent", false, "Run the embedded agent: search, generate, judge, reserve per SKILL.md")
	cmd.Flags().StringVar(&grant, "grant", "ask", "Agent consent for actions beyond this run's scope: ask or auto")
	return cmd
}

func reserve(svc *app.Service, hme, label string, tagList []string, note string, jsonFlag bool, cmd *cobra.Command) error {
	reserved, err := svc.Reserve(hme, label, tagList, note)
	if err != nil {
		return err
	}
	if jsonFlag {
		return cmdutil.OutputResult(cmd, reserved)
	}
	fmt.Printf("Reserved: %s (label: %s)\n", reserved.Hme, reserved.Label)
	return nil
}

func interactive(svc *app.Service, candidates []string, label string, tagList []string, note string, cmd *cobra.Command) error {
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

	return reserve(svc, candidates[idx-1], label, tagList, note, false, cmd)
}
