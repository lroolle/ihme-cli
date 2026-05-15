package new

import (
	"fmt"
	"os"
	"strings"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/tags"
	"github.com/spf13/cobra"
)

func NewCmdNew() *cobra.Command {
	var (
		note    string
		tagList []string
		count   int
		dryRun  bool
	)

	cmd := &cobra.Command{
		Use:   "new <label>",
		Short: "Generate and reserve a new Hide My Email address",
		Long: `Generate a new Hide My Email address and reserve it with the given label.

By default, generates one address and reserves it immediately.
Use -n to generate multiple candidates and pick one interactively.
Use --dry-run to generate without reserving.`,
		Example: `  ihme new github.com
  ihme new github.com --tag dev --note "main account"
  ihme new github.com -n 3
  ihme new github.com --dry-run --json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("label required\n\n  Usage: ihme new <label>\n\n  Example: ihme new github.com --tag dev")
			}
			if len(args) > 1 {
				return fmt.Errorf("too many arguments\n\n  Usage: ihme new <label>\n\n  Got: %s", strings.Join(args, " "))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			label := args[0]

			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")

			if count > 1 {
				return generateMultiple(cmd, client, label, count, dryRun, jsonFlag, tagList, note)
			}

			hme, err := client.GenerateHme()
			if err != nil {
				return err
			}

			if dryRun {
				if jsonFlag {
					fmt.Printf(`{"hme":"%s","label":"%s"}`, hme, label)
					fmt.Println()
				} else {
					fmt.Println(hme)
				}
				return nil
			}

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
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "Note for the address")
	cmd.Flags().StringSliceVar(&tagList, "tag", nil, "Tags (repeatable)")
	cmd.Flags().IntVarP(&count, "count", "n", 1, "Generate N candidates to choose from")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Generate only, don't reserve")
	return cmd
}

func generateMultiple(cmd *cobra.Command, client *api.Client, label string, count int, dryRun, jsonFlag bool, tagList []string, note string) error {
	candidates := make([]string, 0, count)
	for i := 0; i < count; i++ {
		hme, err := client.GenerateHme()
		if err != nil {
			return fmt.Errorf("generating candidate %d: %w", i+1, err)
		}
		candidates = append(candidates, hme)
	}

	if dryRun || jsonFlag {
		if jsonFlag {
			type candidate struct {
				Index int    `json:"index"`
				Hme   string `json:"hme"`
			}
			items := make([]candidate, len(candidates))
			for i, c := range candidates {
				items[i] = candidate{Index: i + 1, Hme: c}
			}
			return cmdutil.OutputResult(cmd, items)
		}
		for _, c := range candidates {
			fmt.Println(c)
		}
		return nil
	}

	fmt.Println("Generated addresses:")
	for i, c := range candidates {
		fmt.Printf("  [%d] %s\n", i+1, c)
	}
	fmt.Printf("Pick one (1-%d): ", count)

	var choice int
	if _, err := fmt.Fscan(os.Stdin, &choice); err != nil || choice < 1 || choice > count {
		return fmt.Errorf("invalid choice — enter a number 1-%d", count)
	}

	selected := candidates[choice-1]
	noteField := tags.Serialize(tagList, note)
	reserved, err := client.ReserveHme(selected, label, noteField)
	if err != nil {
		return err
	}

	fmt.Printf("Reserved: %s (label: %s)\n", reserved.Hme, reserved.Label)
	return nil
}
