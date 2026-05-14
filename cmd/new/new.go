package new

import (
	"fmt"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/tags"
	"github.com/spf13/cobra"
)

func NewCmdNew() *cobra.Command {
	var (
		note    string
		tagList []string
	)

	cmd := &cobra.Command{
		Use:   "new <label>",
		Short: "Generate and reserve a new Hide My Email address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			label := args[0]

			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			hme, err := client.GenerateHme()
			if err != nil {
				return err
			}

			noteField := tags.Serialize(tagList, note)

			reserved, err := client.ReserveHme(hme, label, noteField)
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
