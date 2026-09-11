# Using agents

Run Hide My Email tasks with the built-in assistant, a signed-in coding agent, or an external agent using the CLI.

[Install and sign in first](../README.md#install). Direct CLI commands do not need a model or an API key.

## Use a signed-in coding agent

```bash
ihme agent --via codex "find my github address"
ihme agent --via claude "find my github address"
ihme agent --via opencode "which addresses go to netflix?"
```

The selected coding agent must already be installed and signed in. Node.js and `npx` are needed for the Codex and Claude ACP adapters; OpenCode speaks the [Agent Client Protocol](https://agentclientprotocol.com) directly. `--via` currently runs one task at a time. ihme supplies its tools over MCP and handles consent in your terminal.

## Use your own model key

Create `~/.config/ihme/.env` with your provider key and model. For example:

```dotenv
ANTHROPIC_API_KEY=your-key
ANTHROPIC_MODEL=claude-sonnet-4-6
```

DeepSeek uses `DEEPSEEK_API_KEY` and `DEEPSEEK_MODEL`. An OpenAI-compatible provider uses `OPENAI_API_KEY`, `OPENAI_BASE_URL`, and `OPENAI_MODEL`. Use a model available to your account; keep keys out of version control.

Then run:

```bash
ihme agent                              # interactive assistant
ihme -p "find my github address"        # one task, then exit
ihme new github.com --agent              # choose and reserve for this label
```

The run header shows the effective model and thinking effort. `--effort` sets the reasoning effort where the selected protocol supports it; unsupported chat-completions models show `n/a`.

Provider settings also live in `~/.config/ihme/agent.json`. The CLI detects Messages, Responses, or chat-completions APIs from the endpoint and model, retries supported protocol-mismatch failures, and records learned choices per model under `apis`. An explicit top-level `api` setting takes precedence. See the [release history](../ROADMAP.md) for compatibility changes.

## Consent and address selection

| Entry point | What it grants |
| --- | --- |
| `ihme agent` or a one-shot task | Mutations ask for consent |
| `ihme new <label> --agent` | One reservation for that label; other mutations ask |
| `--grant auto` | Skip consent prompts for this run |

The consent card shows the proposed change. For a reservation, it includes the selected address, label, note, tags, the reason for the choice, and the rejected candidates. Allow once, deny, allow for the run, or type a reply to redirect the agent. The same gate applies when a coding agent supplies the model. Unattended mutations outside the granted scope are denied.

The [skill](../skill/SKILL.md) defines the selection procedure: search for an existing alias, generate candidates, rank the pool, and reserve a choice. The rationale can be as simple as “cleanest of the three.” Recorded preferences guide the ranking; the current request takes precedence.

Candidate refresh is experimental. It reserves and deletes a throwaway address to try to change Apple's repeating pool, so it consumes real API capacity. It requires consent by default and is capped at two refreshes per task. A failed cleanup can leave an address behind. Apple controls whether the pool changes; this is not a bulk generation method.

## Preferences and memory

```bash
ihme memory prefer "Prefer short, easy-to-spell addresses"
ihme memory                             # inspect memory
ihme memory search github
ihme memory graph
ihme memory path                        # locate the files
```

The assistant keeps plain Markdown journals and topic pages. Reservations add history automatically. The `preferences` and `flashcards` pages load into each run; preferences also travel with generated candidate pools, including `ihme new --json`. Relocate memory with `IHME_MEMORY_PATH`, or open the directory in Logseq or Obsidian.

Memory operations report whether a page was created, updated, or reused. Failed writes are reported. Memory can contain account data; do not attach it to bug reports without redacting it.

## Give another agent the skill

```bash
npx skills add lroolle/ihme-cli -g
```

This installs instructions, not the binary. Install ihme and complete `ihme auth login` separately. The [skill](../skill/SKILL.md) describes the procedure; [AGENTS.md](../AGENTS.md) documents command and response shapes.

For a direct MCP client, launch `ihme mcp` as a stdio server after authenticating. It exposes HME tools and memory instructions. Mutations outside the granted scope require consent; unattended requests are denied.

## Data and implementation

Agent runs send the task and tool results to your selected model provider or coding agent. Results can include aliases, labels, notes, and memory. Direct CLI commands do not invoke an LLM. See [Security](../SECURITY.md) for local storage and reporting.

The reusable agent kernel lives in [`pkg/agentkit`](../pkg/agentkit/README.md). It provides the Go tool loop, streaming events, consent gates, and provider clients.
