package root

import (
	"fmt"

	"github.com/lroolle/ihme-cli/cmd/agentcmd"
	"github.com/lroolle/ihme-cli/cmd/auth"
	copycmd "github.com/lroolle/ihme-cli/cmd/copy"
	"github.com/lroolle/ihme-cli/cmd/edit"
	"github.com/lroolle/ihme-cli/cmd/export"
	"github.com/lroolle/ihme-cli/cmd/forward"
	"github.com/lroolle/ihme-cli/cmd/lifecycle"
	"github.com/lroolle/ihme-cli/cmd/list"
	newcmd "github.com/lroolle/ihme-cli/cmd/new"
	"github.com/lroolle/ihme-cli/cmd/view"
	"github.com/spf13/cobra"
)

func NewCmdRoot(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "ihme",
		Short:         "iCloud Hide My Email CLI",
		Long:          "Manage iCloud Hide My Email addresses from the command line.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// `ihme --agent` opens the interactive assistant; bare
		// `ihme` keeps printing help.
		RunE: func(cmd *cobra.Command, args []string) error {
			if agentFlag, _ := cmd.Flags().GetBool("agent"); agentFlag {
				agent := agentcmd.NewCmdAgent()
				agent.SetContext(cmd.Context())
				// Inherit the persistent flags agent expects.
				agent.Flags().AddFlagSet(cmd.PersistentFlags())
				return agent.RunE(agent, args)
			}
			return cmd.Help()
		},
	}

	cmd.Flags().Bool("agent", false, "Open the interactive assistant (same as 'ihme agent')")

	cmd.PersistentFlags().Bool("json", false, "Output as JSON")
	cmd.PersistentFlags().String("jq", "", "Filter JSON output with a jq expression")
	cmd.PersistentFlags().BoolP("verbose", "v", false, "Show request/response details")

	cmd.AddCommand(agentcmd.NewCmdAgent())
	cmd.AddCommand(auth.NewCmdAuth())
	cmd.AddCommand(list.NewCmdList())
	cmd.AddCommand(newcmd.NewCmdNew())
	cmd.AddCommand(view.NewCmdView())
	cmd.AddCommand(edit.NewCmdEdit())
	cmd.AddCommand(copycmd.NewCmdCopy())
	cmd.AddCommand(lifecycle.NewCmdDeactivate())
	cmd.AddCommand(lifecycle.NewCmdReactivate())
	cmd.AddCommand(lifecycle.NewCmdDelete())
	cmd.AddCommand(export.NewCmdExport())
	cmd.AddCommand(forward.NewCmdForward())
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run:   func(cmd *cobra.Command, args []string) { fmt.Printf("ihme %s\n", version) },
	})

	return cmd
}
