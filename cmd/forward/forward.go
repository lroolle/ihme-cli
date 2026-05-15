package forward

import (
	"fmt"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdForward() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forward",
		Short: "Manage forward-to email address",
		Example: `  ihme forward
  ihme forward --json
  ihme forward set user@icloud.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return cmdutil.OutputResult(cmd, map[string]any{
					"forwardTo": result.SelectedFwdTo,
					"available": result.ForwardToEmails,
					"hint":      "ihme forward set <email>",
				})
			}

			fmt.Printf("Forward to: %s\n", result.SelectedFwdTo)
			if len(result.ForwardToEmails) > 1 {
				fmt.Println("Available:")
				for _, e := range result.ForwardToEmails {
					marker := "  "
					if e == result.SelectedFwdTo {
						marker = "* "
					}
					fmt.Printf("  %s%s\n", marker, e)
				}
			}
			return nil
		},
	}

	cmd.AddCommand(newCmdForwardSet())
	return cmd
}

func newCmdForwardSet() *cobra.Command {
	return &cobra.Command{
		Use:     "set <email>",
		Short:   "Change forward-to email address",
		Example: "  ihme forward set user@icloud.com",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("email required\n\n  Usage: ihme forward set <email>\n  Example: ihme forward set user@icloud.com")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			if err := client.UpdateForwardTo(args[0]); err != nil {
				return err
			}

			fmt.Printf("Forward-to updated to %s\n", args[0])
			return nil
		},
	}
}
