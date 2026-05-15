package reserve

import (
	"fmt"
	"strings"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/tags"
	"github.com/spf13/cobra"
)

func NewCmdReserve() *cobra.Command {
	var (
		note    string
		tagList []string
	)

	cmd := &cobra.Command{
		Use:   "reserve <address> <label>",
		Short: "Reserve a previously generated address",
		Long: `Reserve a Hide My Email address that was generated with 'ihme generate'.
The address must be a valid unreserved candidate from a recent generate call.`,
		Example: `  ihme reserve abc123@icloud.com github.com
  ihme reserve abc123@icloud.com github.com --tag dev
  ihme reserve abc123@icloud.com github.com --json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("address and label required\n\n  Usage: ihme reserve <address> <label>\n  Example: ihme reserve abc123@icloud.com github.com")
			}
			if len(args) > 2 {
				return fmt.Errorf("too many arguments\n\n  Usage: ihme reserve <address> <label>\n  Got: %s", strings.Join(args, " "))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			address := args[0]
			label := args[1]

			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			noteField := tags.Serialize(tagList, note)
			reserved, err := client.ReserveHme(address, label, noteField)
			if err != nil {
				return err
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return cmdutil.OutputResult(cmd, reserved)
			}
			fmt.Printf("Reserved: %s (label: %s)\n", reserved.Hme, reserved.Label)
			return nil
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "Note for the address")
	cmd.Flags().StringSliceVar(&tagList, "tag", nil, "Tags (repeatable)")
	return cmd
}
