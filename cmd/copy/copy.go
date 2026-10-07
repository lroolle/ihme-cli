package copy

import (
	"fmt"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/clip"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/resolver"

	"github.com/spf13/cobra"
)

func NewCmdCopy() *cobra.Command {
	return &cobra.Command{
		Use:   "copy [ref]",
		Short: "Copy a Hide My Email address to clipboard",
		Long: `Copy a Hide My Email address to the clipboard (printed when no
clipboard is available). Without <ref> at a terminal, asks which
active address, newest first; Enter takes the one just created.`,
		Aliases: []string{"cp"},
		Example: "  ihme copy github.com\n  ihme copy                # pick, newest first",
		Args:    cmdutil.RefOrPick("ihme copy <ref>", "ihme copy github.com"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}
			ref, err := cmdutil.RefFromArgs(args, client, cmdutil.Pick{
				Verb: "copy",
				Want: func(e api.HmeEmail) bool { return e.IsActive },
				None: "no active addresses to copy — create one with: ihme new <label>",
			})
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			hme, err := resolver.Resolve(ref, result.HmeEmails)
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
