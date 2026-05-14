package root

import (
	"github.com/spf13/cobra"
)

func NewCmdRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ihme",
		Short: "iCloud Hide My Email CLI",
		Long:  "Manage iCloud Hide My Email addresses from the command line.",
	}

	// TODO: register subcommands
	// cmd.AddCommand(auth.NewCmdAuth())
	// cmd.AddCommand(list.NewCmdList())
	// cmd.AddCommand(newcmd.NewCmdNew())
	// cmd.AddCommand(view.NewCmdView())
	// cmd.AddCommand(edit.NewCmdEdit())
	// cmd.AddCommand(export.NewCmdExport())

	cmd.PersistentFlags().Bool("json", false, "Output as JSON")
	cmd.PersistentFlags().String("jq", "", "Filter JSON output with a jq expression")

	return cmd
}
