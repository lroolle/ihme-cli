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
	)

	cmd := &cobra.Command{
		Use:   "new <label>",
		Short: "Generate and reserve a new Hide My Email address",
		Long: `Generate a new Hide My Email address and reserve it with the given label.

Shows the generated address and lets you confirm, regenerate for a
different one, or cancel. Use --yes to skip confirmation (for scripts
and agents).`,
		Example: `  ihme new github.com
  ihme new github.com --tag dev --note "main account"
  ihme new github.com --yes
  ihme new github.com --yes --json`,
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
			jsonFlag, _ := cmd.Flags().GetBool("json")

			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			interactive := !yes && !jsonFlag && term.IsTerminal(int(os.Stdin.Fd()))

			hme, err := client.GenerateHme()
			if err != nil {
				return err
			}

			if interactive {
				hme, err = confirmLoop(client, hme)
				if err != nil {
					return err
				}
				if hme == "" {
					fmt.Println("Cancelled.")
					return nil
				}
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
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation, reserve immediately")
	return cmd
}

func confirmLoop(client *api.Client, hme string) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("\n  %s\n\n", hme)
		fmt.Print("Use this address? [y]es / [r]egenerate / [c]ancel: ")

		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		choice := strings.TrimSpace(strings.ToLower(line))

		switch {
		case choice == "y" || choice == "yes" || choice == "":
			return hme, nil
		case choice == "r" || choice == "regenerate":
			next, err := client.GenerateHme()
			if err != nil {
				return "", fmt.Errorf("regenerating: %w", err)
			}
			hme = next
		case choice == "c" || choice == "cancel" || choice == "n" || choice == "no":
			return "", nil
		default:
			fmt.Println("  Enter y, r, or c")
		}
	}
}
