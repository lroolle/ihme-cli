package copy

import (
	"fmt"

	"github.com/lroolle/ihme-cli/internal/clip"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/resolver"

	"github.com/spf13/cobra"
)

func NewCmdCopy() *cobra.Command {
	return &cobra.Command{
		Use:     "copy <ref>",
		Short:   "Copy a Hide My Email address to clipboard",
		Aliases: []string{"cp"},
		Example: "  ihme copy github.com",
		Args:    cmdutil.ExactRefArg("ihme copy <ref>", "ihme copy github.com"),
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

			if err := clip.Copy(hme.Hme); err != nil {
				fmt.Println(hme.Hme)
				return nil
			}

			fmt.Printf("Copied %s to clipboard\n", hme.Hme)
			return nil
		},
	}
}
