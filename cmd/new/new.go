package new

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/tags"
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
	)

	cmd := &cobra.Command{
		Use:   "new <label>",
		Short: "Generate and reserve a new Hide My Email address",
		Long: `Generate Hide My Email address candidates and reserve one.

Matches the iCloud web flow: generate first, pick, then reserve.

  --json           Show candidates without reserving (for agents)
  --address <addr> Reserve a specific previously-generated address
  --yes            Generate one and reserve immediately`,
		Example: `  ihme new github.com
  ihme new github.com --json
  ihme new github.com --address pole-toils-3x@icloud.com
  ihme new github.com -y --json
  ihme new github.com --tag dev --note "main account"`,
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

			noteField := tags.Serialize(tagList, note)

			// --address: reserve a specific address (agent step 2)
			if address != "" {
				return reserve(client, address, label, noteField, jsonFlag, cmd)
			}

			// --yes: generate one, reserve, done (script mode)
			if yes {
				hme, err := client.GenerateHme()
				if err != nil {
					return err
				}
				return reserve(client, hme, label, noteField, jsonFlag, cmd)
			}

			candidates, err := generateN(client, count)
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
				return reserve(client, candidates[0], label, noteField, false, cmd)
			}

			// interactive: pick from list
			return interactive(client, candidates, label, noteField, cmd)
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "Note for the address")
	cmd.Flags().StringSliceVar(&tagList, "tag", nil, "Tags (repeatable)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Reserve the first candidate immediately")
	cmd.Flags().StringVar(&address, "address", "", "Reserve a specific generated address")
	cmd.Flags().IntVarP(&count, "count", "n", 3, "Number of candidates to generate")
	return cmd
}

func reserve(client *api.Client, hme, label, noteField string, jsonFlag bool, cmd *cobra.Command) error {
	reserved, err := client.ReserveHme(hme, label, noteField)
	if err != nil {
		return err
	}
	if jsonFlag {
		return cmdutil.OutputResult(cmd, reserved)
	}
	fmt.Printf("Reserved: %s (label: %s)\n", reserved.Hme, reserved.Label)
	return nil
}

func interactive(client *api.Client, candidates []string, label, noteField string, cmd *cobra.Command) error {
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

	return reserve(client, candidates[idx-1], label, "", false, cmd)
}

func generateN(client *api.Client, n int) ([]string, error) {
	seen := make(map[string]bool)
	var candidates []string
	dupeStreak := 0
	for dupeStreak < 3 && len(candidates) < n {
		hme, err := client.GenerateHme()
		if err != nil {
			if len(candidates) > 0 {
				break
			}
			return nil, err
		}
		if seen[hme] {
			dupeStreak++
			continue
		}
		seen[hme] = true
		dupeStreak = 0
		candidates = append(candidates, hme)
	}
	return candidates, nil
}
