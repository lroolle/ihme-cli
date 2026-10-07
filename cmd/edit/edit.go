package edit

import (
	"fmt"

	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdEdit() *cobra.Command {
	var (
		label   string
		note    string
		tagList []string
	)

	cmd := &cobra.Command{
		Use:   "edit [ref]",
		Short: "Edit label, note, or tags of a Hide My Email address",
		Long: `Edit metadata of a Hide My Email address.

Only specified flags are changed; omitted fields keep their current value.
Tags replace all existing tags (not additive).
Without <ref> at a terminal, asks which address, newest first.`,
		Example: `  ihme edit github.com --label GitHub
  ihme edit github.com --tag dev,work --note "main"
  ihme edit github.com --tag ""`,
		Args: cmdutil.RefOrPick("ihme edit <ref>", "ihme edit github.com --label GitHub"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("label") && !cmd.Flags().Changed("note") && !cmd.Flags().Changed("tag") {
				return fmt.Errorf("nothing to change — pass --label, --note, or --tag\n\n  Example: ihme edit github.com --label GitHub")
			}
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}
			ref, err := cmdutil.RefFromArgs(args, client, cmdutil.Pick{
				Verb: "edit",
				None: "no addresses yet — create one with: ihme new <label>",
			})
			if err != nil {
				return err
			}

			var patch app.MetaPatch
			if cmd.Flags().Changed("label") {
				patch.Label = &label
			}
			if cmd.Flags().Changed("note") {
				patch.Note = &note
			}
			if cmd.Flags().Changed("tag") {
				patch.Tags = &tagList
			}

			hme, err := app.New(client).UpdateMeta(ref, patch)
			if err != nil {
				return err
			}

			fmt.Printf("Updated %s\n", hme.Hme)
			return nil
		},
	}

	cmd.Flags().StringVar(&label, "label", "", "New label")
	cmd.Flags().StringVar(&note, "note", "", "New note")
	cmd.Flags().StringSliceVar(&tagList, "tag", nil, "Tags (replaces existing)")
	return cmd
}
