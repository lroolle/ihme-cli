package view

import (
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/resolver"
	"github.com/spf13/cobra"
)

func NewCmdView() *cobra.Command {
	return &cobra.Command{
		Use:   "view <ref>",
		Short: "View details of a Hide My Email address",
		Long:  "<ref> can be an anonymousId, email address, or label (fuzzy match).",
		Example: "  ihme view github.com\n  ihme view abc123@privaterelay.appleid.com --json",
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

			return cmdutil.OutputResult(cmd, hme)
		},
	}
}
