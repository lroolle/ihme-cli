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
	var grant, effort, prompt, via string

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
and put the key in ~/.config/ihme/.env (or export it). A vendor key
plus a model name is a complete configuration: ANTHROPIC_API_KEY +
ANTHROPIC_MODEL=claude-…, or DEEPSEEK_API_KEY +
DEEPSEEK_MODEL=deepseek-v4-flash. The wire protocol
(/chat/completions vs /responses vs Messages) is detected
automatically and remembered; pin it with "api" if your endpoint
misbehaves.

No API key? --via harnesses a full coding agent you are already
signed in to as the provider (its subscription auth, its models):
  ihme agent --via codex  "new address for github"   # ChatGPT plan
  ihme agent --via claude "new address for github"   # Claude plan
The guest gets ihme's operations as MCP tools (caps, taste
rationale, and memory journaling all still enforced), and every
mutation still lands on your consent card. One-shot tasks only for
now; codex/claude go through their ACP adapters (fetched via npx on
first use), opencode speaks ACP natively.

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

			if via != "" {
				if effort != "" {
					return fmt.Errorf("--effort configures the BYOK embedded agent; with --via, configure the %s agent itself", via)
				}
				if task == "" {
					return fmt.Errorf("interactive --via sessions are not supported yet — give a task: ihme agent --via %s \"<task>\"", via)
				}
				jsonFlag, _ := cmd.Flags().GetBool("json")
				verbose, _ := cmd.Flags().GetBool("verbose")
				res, err := agent.RunVia(cmd.Context(), task, via, agent.GrantMode(grant), jsonFlag, verbose)
				if err != nil {
					return err
				}
				if jsonFlag {
					return cmdutil.OutputResult(cmd, res)
				}
				return nil
			}

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
	cmd.Flags().StringVar(&effort, "effort", "", "Reasoning effort: minimal, low, medium, high (claude 4.6+ and deepseek also: xhigh, max; older claude models map it to a thinking budget)")
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Run one task and exit (same as a positional task)")
	cmd.Flags().StringVar(&via, "via", "", "Harness a full coding agent as the provider: codex, claude, or opencode (uses its subscription auth, no API key)")
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
