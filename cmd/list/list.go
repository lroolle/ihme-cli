package list

import (
	"fmt"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/filter"
	"github.com/spf13/cobra"
)

func NewCmdList() *cobra.Command {
	var opts filter.Options

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List Hide My Email addresses",
		Aliases: []string{"ls"},
		Example: `  ihme list
  ihme list --active
  ihme list --tag dev
  ihme list --json
  ihme list --json --jq '.[].hme'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			emails := filter.Apply(result.HmeEmails, opts)

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				out := map[string]any{
					"addresses": emails,
					"count":     len(emails),
					"hints": map[string]string{
						"view":       "ihme view <label-or-id> --json",
						"edit":       "ihme edit <label-or-id> --label <new-label>",
						"new":        "ihme new <label> --json",
						"deactivate": "ihme deactivate <label-or-id>",
						"export":     fmt.Sprintf("ihme export (total: %d)", len(emails)),
					},
				}
				return cmdutil.OutputResult(cmd, out)
			}
			return cmdutil.OutputResult(cmd, emails)
		},
	}

	cmd.Flags().BoolVar(&opts.Active, "active", false, "Show only active addresses")
	cmd.Flags().BoolVar(&opts.Inactive, "inactive", false, "Show only inactive addresses")
	cmd.Flags().StringVar(&opts.Tag, "tag", "", "Filter by tag")
	return cmd
}
