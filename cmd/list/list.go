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
		Long: `List Hide My Email addresses with optional filters.

JSON output (--json):
  {
    "addresses": [{"anonymousId","label","hme","isActive","createTimestamp","note",...}],
    "count": 325,
    "hints": {"view":"ihme view <ref> --json", ...}
  }

Use --jq to query: ihme list --json --jq '.addresses[] | select(.isActive)'`,
		Example: `  ihme list
  ihme list --active
  ihme list --search netflix
  ihme list --search github --active --json
  ihme list --json --jq '.addresses[0:5]'
  ihme list --sort label`,
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
	cmd.Flags().StringVarP(&opts.Search, "search", "s", "", "Search label, address, or note")
	cmd.Flags().StringVar(&opts.Sort, "sort", "", "Sort by: date, label, date:asc, label:desc")
	return cmd
}
