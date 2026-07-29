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
	var grant, effort, prompt string

	cmd := &cobra.Command{
		Use:   "agent [task...]",
		Short: "Talk to the embedded assistant (interactive without arguments)",
		Long: `Run the embedded assistant over your Hide My Email addresses.

Without arguments: an interactive session. With arguments (or
--prompt/-p): one task, run immediately.

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
  ihme agent -p "new address for github signup"
  ihme agent --prompt "which addresses go to netflix?"
  ihme agent --grant auto "retag every dev address as #work"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if grant != string(agent.GrantAsk) && grant != string(agent.GrantAuto) {
				return fmt.Errorf("invalid --grant %q — use ask or auto", grant)
			}
			task, err := resolveTask(prompt, args)
			if err != nil {
				return err
			}
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}
			svc := app.New(client)
			appleID := client.Session().AppleID

			if task == "" {
				return agent.RunREPL(cmd.Context(), svc, appleID, agent.GrantMode(grant), effort)
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			res, err := agent.RunTask(cmd.Context(), svc, appleID, task, agent.GrantMode(grant), effort, jsonFlag)
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
	cmd.Flags().StringVar(&effort, "effort", "", "Reasoning effort: minimal, low, medium, high (claude 4.6+ also: xhigh, max; older claude models map it to a thinking budget)")
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Run one task and exit (same as a positional task)")
	return cmd
}

// resolveTask merges the two ways to hand the agent a one-shot task.
// Both at once is refused rather than silently joined or dropped.
func resolveTask(prompt string, args []string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	argTask := strings.TrimSpace(strings.Join(args, " "))
	if prompt != "" && argTask != "" {
		return "", fmt.Errorf("task given twice — use --prompt %q or the positional task %q, not both", prompt, argTask)
	}
	if prompt != "" {
		return prompt, nil
	}
	return argTask, nil
}
