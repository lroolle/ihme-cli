package view

import (
	"fmt"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/resolver"
	"github.com/spf13/cobra"
)

func NewCmdView() *cobra.Command {
	return &cobra.Command{
		Use:   "view <ref>",
		Short: "View details of a Hide My Email address",
		Long:  "<ref> can be an anonymousId, email address, or label (fuzzy match).",
		Example: `  ihme view github.com
  ihme view github.com --json
  ihme view abc123@privaterelay.appleid.com`,
		Args:    cmdutil.ExactRefArg("ihme view <ref>", "ihme view github.com"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			hme, err := resolver.Resolve(args[0], result.HmeEmails)
			if err != nil {
				return err
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				out := map[string]any{
					"result": hme,
					"hints": map[string]string{
						"edit":       fmt.Sprintf("ihme edit %s --label <new-label>", hme.AnonymousID),
						"deactivate": fmt.Sprintf("ihme deactivate %s", hme.AnonymousID),
						"delete":     fmt.Sprintf("ihme delete %s --yes", hme.AnonymousID),
					},
				}
				return cmdutil.OutputResult(cmd, out)
			}
			return cmdutil.OutputResult(cmd, hme)
		},
	}
}
