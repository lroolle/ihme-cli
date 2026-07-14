// Package agentcmd exposes the embedded general assistant:
// `ihme agent` for an interactive session, `ihme agent <task>` for
// one shot. `ihme --agent` aliases the interactive form.
package agentcmd

import (
	"fmt"
	"strings"

	"github.com/lroolle/ihme-cli/internal/agent"
	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdAgent() *cobra.Command {
	var grant, effort string

	cmd := &cobra.Command{
		Use:   "agent [task...]",
		Short: "Talk to the embedded assistant (interactive without arguments)",
		Long: `Run the embedded assistant over your Hide My Email addresses.

Without arguments: an interactive session. With arguments: one task.

Every action that changes anything — reserving, deactivating,
editing — asks for your consent first. --grant auto skips the
asking (use deliberately).

BYOK: configure the model in ~/.config/ihme/agent.json
  {"model":"...","baseUrl":"https://api.example.com/v1",
   "apiKeyEnv":"OPENAI_API_KEY"}
and put the key in ~/.config/ihme/.env (or export it). The wire
protocol (/chat/completions vs /responses) is detected automatically
and remembered; pin it with "api": "completions"|"responses" if
your endpoint misbehaves.

JSON output (--json, one-shot only):
  {"reserved":{...}|null,"summary":"...","transcript":[...],"usage":{...}}`,
		Example: `  ihme agent
  ihme agent "new address for github signup"
  ihme agent "which addresses go to netflix?"
  ihme agent --grant auto "retag every dev address as #work"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if grant != string(agent.GrantAsk) && grant != string(agent.GrantAuto) {
				return fmt.Errorf("invalid --grant %q — use ask or auto", grant)
			}
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}
			svc := app.New(client)
			appleID := client.Session().AppleID

			if len(args) == 0 {
				return agent.RunREPL(cmd.Context(), svc, appleID, agent.GrantMode(grant), effort)
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			res, err := agent.RunTask(cmd.Context(), svc, appleID, strings.Join(args, " "), agent.GrantMode(grant), effort, jsonFlag)
			if err != nil {
				return err
			}
			if jsonFlag {
				return cmdutil.OutputResult(cmd, res)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&grant, "grant", "ask", "Consent for mutating actions: ask or auto")
	cmd.Flags().StringVar(&effort, "effort", "", "Reasoning effort for responses-API models: minimal, low, medium, high")
	return cmd
}
