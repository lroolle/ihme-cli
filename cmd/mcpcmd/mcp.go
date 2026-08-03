// Package mcpcmd exposes the agent tool layer as an MCP stdio
// server. Hidden: it exists to be spawned by an agent harness (the
// `--via` flow hands it to the guest in session/new), not to be a
// user-facing surface.
package mcpcmd

import (
	"fmt"
	"os"

	"github.com/lroolle/ihme-cli/internal/agent"
	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdMCP(version string) *cobra.Command {
	var grant, consentSock string
	cmd := &cobra.Command{
		Use:    "mcp",
		Short:  "Serve the agent tools over the Model Context Protocol (stdio)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if grant != string(agent.GrantAsk) && grant != string(agent.GrantAuto) {
				return fmt.Errorf("invalid --grant %q — use ask or auto", grant)
			}
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}
			return agent.ServeMCP(cmd.Context(), app.New(client),
				client.Session().AppleID, version, agent.GrantMode(grant),
				consentSock, os.Stdin, os.Stdout)
		},
	}
	cmd.Flags().StringVar(&grant, "grant", "ask", "Consent for mutating actions: ask (deny without a consent channel) or auto")
	cmd.Flags().StringVar(&consentSock, "consent-socket", "", "Unix socket where an ihme harness answers consent cards (set by --via)")
	return cmd
}
