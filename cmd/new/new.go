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
		pick    int
		count   int
	)

	cmd := &cobra.Command{
		Use:   "new <label>",
		Short: "Generate and reserve a new Hide My Email address",
		Long: `Generate Hide My Email address candidates and reserve one.

Shows candidates and lets you pick interactively. Apple's pool
rotates ~3 unique addresses.

For agents: --pick N generates candidates and reserves the Nth one
in a single call — no interactive prompt, no separate commands.`,
		Example: `  ihme new github.com
  ihme new github.com -y
  ihme new github.com --pick 2
  ihme new github.com --pick 1 --json
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

			if yes {
				return generateAndReserve(client, 1, 1, label, tagList, note, jsonFlag, cmd)
			}

			if pick > 0 {
				return generateAndReserve(client, count, pick, label, tagList, note, jsonFlag, cmd)
			}

			if !term.IsTerminal(int(os.Stdin.Fd())) || jsonFlag {
				return generateAndReserve(client, 1, 1, label, tagList, note, jsonFlag, cmd)
			}

			return interactiveNew(client, count, label, tagList, note, cmd)
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "Note for the address")
	cmd.Flags().StringSliceVar(&tagList, "tag", nil, "Tags (repeatable)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Reserve the first candidate immediately")
	cmd.Flags().IntVar(&pick, "pick", 0, "Generate candidates and reserve the Nth one")
	cmd.Flags().IntVarP(&count, "count", "n", 3, "Number of candidates to generate")
	return cmd
}

func generateAndReserve(client *api.Client, n, pick int, label string, tagList []string, note string, jsonFlag bool, cmd *cobra.Command) error {
	candidates, err := generateN(client, n)
	if err != nil {
		return err
	}

	if pick < 1 || pick > len(candidates) {
		pick = 1
	}

	selected := candidates[pick-1]
	noteField := tags.Serialize(tagList, note)
	reserved, err := client.ReserveHme(selected, label, noteField)
	if err != nil {
		return err
	}

	if jsonFlag {
		return cmdutil.OutputResult(cmd, reserved)
	}
	fmt.Printf("Reserved: %s (label: %s)\n", reserved.Hme, reserved.Label)
	return nil
}

func interactiveNew(client *api.Client, n int, label string, tagList []string, note string, cmd *cobra.Command) error {
	candidates, err := generateN(client, n)
	if err != nil {
		return err
	}

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

	noteField := tags.Serialize(tagList, note)
	reserved, err := client.ReserveHme(candidates[idx-1], label, noteField)
	if err != nil {
		return err
	}

	fmt.Printf("Reserved: %s (label: %s)\n", reserved.Hme, reserved.Label)
	return nil
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
