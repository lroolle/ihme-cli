package list

import (
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
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			return cmdutil.OutputResult(cmd, filter.Apply(result.HmeEmails, opts))
		},
	}

	cmd.Flags().BoolVar(&opts.Active, "active", false, "Show only active addresses")
	cmd.Flags().BoolVar(&opts.Inactive, "inactive", false, "Show only inactive addresses")
	cmd.Flags().StringVar(&opts.Tag, "tag", "", "Filter by tag")
	return cmd
}
