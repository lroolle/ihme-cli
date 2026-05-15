package edit

import (
	"fmt"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/resolver"
	"github.com/lroolle/ihme-cli/pkg/tags"
	"github.com/spf13/cobra"
)

func NewCmdEdit() *cobra.Command {
	var (
		label   string
		note    string
		tagList []string
	)

	cmd := &cobra.Command{
		Use:   "edit <ref>",
		Short: "Edit label, note, or tags of a Hide My Email address",
		Long: `Edit metadata of a Hide My Email address.

Only specified flags are changed; omitted fields keep their current value.
Tags replace all existing tags (not additive).`,
		Example: `  ihme edit github.com --label GitHub
  ihme edit github.com --tag dev,work --note "main"
  ihme edit github.com --tag ""`,
		Args:    cmdutil.ExactRefArg("ihme edit <ref>", "ihme edit github.com --label GitHub"),
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

			newLabel := hme.Label
			if cmd.Flags().Changed("label") {
				newLabel = label
			}

			parsed := tags.Parse(hme.Note)
			newNote := parsed.Note
			if cmd.Flags().Changed("note") {
				newNote = note
			}

			newTags := parsed.Tags
			if cmd.Flags().Changed("tag") {
				newTags = tagList
			}

			noteField := tags.Serialize(newTags, newNote)

			if err := client.UpdateHmeMetadata(hme.AnonymousID, newLabel, noteField); err != nil {
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
