package generate

import (
	"fmt"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdGenerate() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a candidate address without reserving",
		Long: `Generate a Hide My Email address candidate. Does NOT reserve it.
Call multiple times to get different candidates, then use
'ihme reserve <address> <label>' to claim the one you want.`,
		Example: `  ihme generate
  ihme generate --json
  ihme generate --json | jq -r .hme`,
		Aliases: []string{"gen"},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			hme, err := client.GenerateHme()
			if err != nil {
				return err
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				fmt.Printf(`{"hme":"%s"}`, hme)
				fmt.Println()
				return nil
			}

			fmt.Println(hme)
			return nil
		},
	}

	return cmd
}
