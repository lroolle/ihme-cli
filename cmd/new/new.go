package new

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/tags"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const defaultCandidates = 3

func NewCmdNew() *cobra.Command {
	var (
		note    string
		tagList []string
		yes     bool
		count   int
	)

	cmd := &cobra.Command{
		Use:   "new <label>",
		Short: "Generate and reserve a new Hide My Email address",
		Long: `Generate Hide My Email address candidates and reserve one.

By default generates 3 candidates (Apple's rotation pool) and lets
you pick. Use --yes to skip selection and reserve the first one.

With --json, returns candidates for agent selection — then call
'ihme reserve <address> <label>' to claim one.`,
		Example: `  ihme new github.com
  ihme new github.com --yes
  ihme new github.com --tag dev --note "main account"
  ihme new github.com --json
  ihme new github.com -n 5`,
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

			n := count
			if yes {
				n = 1
			}

			candidates, err := generateN(client, n)
			if err != nil {
				return err
			}

			// Agent mode: return candidates as JSON for external selection
			if jsonFlag && !yes {
				out := map[string]any{
					"candidates": candidates,
					"label":      label,
					"hint":       fmt.Sprintf("ihme reserve <address> %s", label),
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			// Script mode: take first
			if yes {
				return reserveAndPrint(client, candidates[0], label, tagList, note, jsonFlag, cmd)
			}

			// Interactive mode: pick from list
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return reserveAndPrint(client, candidates[0], label, tagList, note, jsonFlag, cmd)
			}

			selected, err := pickInteractive(candidates)
			if err != nil {
				return err
			}
			if selected == "" {
				fmt.Println("Cancelled.")
				return nil
			}

			return reserveAndPrint(client, selected, label, tagList, note, jsonFlag, cmd)
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "Note for the address")
	cmd.Flags().StringSliceVar(&tagList, "tag", nil, "Tags (repeatable)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Reserve the first candidate immediately")
	cmd.Flags().IntVarP(&count, "count", "n", defaultCandidates, "Number of candidates to generate")
	return cmd
}

func generateN(client *api.Client, n int) ([]string, error) {
	seen := make(map[string]bool)
	var candidates []string
	dupeStreak := 0
	// Apple's pool is small (~3). Stop early if we keep getting duplicates
	// rather than hammering the server.
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

func pickInteractive(candidates []string) (string, error) {
	fmt.Println()
	for i, c := range candidates {
		fmt.Printf("  [%d] %s\n", i+1, c)
	}
	fmt.Printf("\nSelect [1-%d] or [c]ancel: ", len(candidates))

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	choice := strings.TrimSpace(strings.ToLower(line))

	if choice == "c" || choice == "cancel" {
		return "", nil
	}

	var idx int
	if _, err := fmt.Sscan(choice, &idx); err != nil || idx < 1 || idx > len(candidates) {
		return "", fmt.Errorf("invalid choice — enter 1-%d or c", len(candidates))
	}
	return candidates[idx-1], nil
}

func reserveAndPrint(client *api.Client, hme, label string, tagList []string, note string, jsonFlag bool, cmd *cobra.Command) error {
	noteField := tags.Serialize(tagList, note)
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
