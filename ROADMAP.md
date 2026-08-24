# Roadmap

Current: **v0.6.3** · [MIT](LICENSE)

## Shipped in v0.6.3 — the session heals itself, and errors say so

- **A 401 mid-command no longer reaches the user.** The mail-domain
  host keeps its own cookies, so it can refuse a session `/validate`
  just cleared — the pre-flight check cannot see that coming. HME
  calls now re-mint once through `accountLogin` (not `validate`,
  which already lied), rebuild the URL from the fresh webservices
  map and dsid, and replay the call; a rejected call changed nothing
  on Apple's side, so replaying is safe even for mutations. One shot
  only, and the fresh cookies land back on disk.
- **2FA survives Apple's 409.** Since ~mid-2026 the securitycode
  endpoints answer 409 to a *valid* code; the acceptance signal is
  the fresh `X-Apple-Session-Token`, not the status. `ihme auth
  login` used to die there (same break as rclone#9488).
- **Errors are a decision, not a dump.** One place renders them
  (`cmdutil.Explain`) and one maps exit codes (`cmdutil.ExitCode`):
  what happened, the cause, the command that fixes it — and exit 2
  for "authenticate" from every command, not just `auth status`.
  Account identifiers (dsid, clientId) are stripped from error text
  and `-v` logs, so a pasted error is safe.
- **Session writes are atomic** (write-then-rename) and `clientId`
  is generated once per installation instead of per invocation.
- **`refresh_candidates` rejoins the consent gate**, and taste stops
  demanding poetry. Net-zero described ACCOUNT state, never API
  pressure, and the per-task cap resets every turn — so a model with
  a miscalibrated bar burned throwaways task after task. The card now
  carries the model's per-candidate verdict; the prompt says taste
  RANKS a pool and rarely vetoes one; the rationale is one honest
  sentence, with an image only when there is one (TASTE.md,
  2026-08-12 and 2026-08-13).

## Shipped in v0.6.0 — harness a coding agent as the provider

- **`ihme agent --via codex|claude|opencode "<task>"`.** The
  provider relationship inverts: ihme is the harness (an [Agent
  Client Protocol](https://agentclientprotocol.com) client,
  `internal/acp` — a hand-rolled v1 subset in the agentkit spirit),
  the coding agent you are already signed in to is the brain. Its
  subscription auth, its models, no API key. The guest gets the HME
  operations back as MCP tools by re-invoking this binary
  (`ihme mcp`, hidden), so the rationale floor, rate caps, and
  memory journaling hold no matter who drives.
- **The consent card does not move to the guest.** Verified live:
  guest permission layers execute mutating MCP tools without asking
  (claude-agent-acp issued zero `session/request_permission` for a
  deactivate). So the scoped-consent gate runs inside the MCP
  server process, and its cards travel over a unix socket to the
  harness terminal — same card, same y/N/a-or-reply protocol; a
  typed reply rides the denial back to the guest as in-turn
  direction. Unattended runs deny mutations with an adaptable
  reason, exactly like the embedded agent.
- **deepseek-v4 as a first-class BYOK provider.** DeepSeek ships
  native /responses support in the v4 generation:
  `DEEPSEEK_API_KEY` + `DEEPSEEK_MODEL=deepseek-v4-flash` is now a
  complete configuration (base URL and key env default per vendor),
  auto-detection starts `deepseek-v4*` on the responses API
  (earlier generations stay on chat completions), and `--effort`
  passes through. Verified live against api.deepseek.com.
- One-shot tasks only for `--via` so far; the interactive REPL
  stays BYOK. Codex/claude adapters are fetched via `npx` on first
  use; opencode speaks ACP natively.

## Shipped in v0.5.0 — claude and codex as first-class providers

- **Native Anthropic Messages API** (`pkg/agentkit/ai/anthropic`):
  point the agent at a Claude model and `ANTHROPIC_API_KEY` +
  `ANTHROPIC_MODEL` is a complete configuration — the base URL
  defaults to Anthropic's endpoint. The shared `--effort` vocabulary
  applies generationally: `output_config` effort on Claude 4.6+
  (where manual budgets are a 400), a manual thinking budget on
  older models. Thinking blocks (signatures included) round-trip
  verbatim and stream live like responses-API reasoning summaries;
  a safety refusal surfaces as an error, never as a clean finish.
- **Auto-detection covers three protocols.** `claude*` models and
  anthropic.com endpoints start on the Messages API; a claude model
  behind an OpenAI-protocol gateway self-heals to chat completions
  on the first 404, and the discovery persists. Codex/gpt-5/o-series
  keep starting on the responses API — codex as a provider already
  worked; now it is documented.

## Shipped in v0.4.0 — agent polish

- **`--prompt`/`-p`.** Direct one-shot execution: `ihme agent -p
  "<task>"`, `ihme --agent -p "<task>"`, or just `ihme -p "<task>"` —
  no interactive prompt first. Positional task words still work;
  giving both is refused, not guessed at.
- **Memory operations are visible.** Reservation-time journaling,
  `remember`, and `recall_memory` each print what actually happened:
  `Memory created/updated for "x"`, `Reused memory for "x"`, and a
  failed write says so instead of staying silent.
- **The run states its configuration.** Every agent run opens with
  `Model:` and `Thinking effort:` as effectively resolved (config
  file, env, flags); effort on chat-completions models is reported as
  not applicable, never echoed as if applied.

## Shipped in v0.3.0 — the agent gets memory and judgment

- **Consent is a conversation.** The consent card carries the
  agent's taste verdict and the candidates it rejected; you can type
  a reply to redirect it, not just allow/deny.
- **Agent memory.** A Logseq-style markdown graph (`ihme memory`)
  the agent keeps across runs — reservations journal themselves,
  topic pages accumulate a service's history, a `flashcards` page
  loads into every run. Plain files, no database.
- **Candidate refresh** *(experimental)*. When a pool is weak and
  Apple keeps returning the same options, the agent burns a
  throwaway (reserve → deactivate → delete) to force a fresh pool.
  Bounded, net-zero on the common path, pending real-world
  validation of Apple's pending-pool behavior.
- **Harness fix.** Per-task rate budgets reset each interactive
  request, so a long session no longer permanently exhausts its
  generation budget.

## Shipped in v0.2.0 — the embedded agent

Not on the original roadmap; it emerged and took the release:

- `ihme new <label> --agent` and `ihme agent` — a built-in BYOK
  assistant over `pkg/agentkit`, a stdlib-only reusable agent kernel
- Scoped consent (a task pre-grants exactly its own scope), inline
  TUI with three-choice consent, live reasoning, loud taste rationale
- Wire-protocol auto-detection (chat completions vs responses API)
- From the session-resilience list, delivered early: session-resume
  failure classification (transient ≠ expired; no more surprise
  sign-in emails), one quiet retry on transport trouble, 15-min
  validation freshness window
- From the agent-native list, delivered differently: taste scoring
  became the agent's mandatory reserve rationale instead of a
  `--taste` flag
- Agent memory (`ihme memory`): a Logseq-style markdown graph the
  agent keeps across runs — reservations journal themselves, topic
  pages accumulate a service's history, a flashcards page loads into
  every run. Plain files, openable in Logseq/Obsidian, no database

## v0.3 — Fix what the review found

| Item | Why |
|------|-----|
| Silent 2FA push failure — log error, fall back to code prompt | Users wait forever when push silently fails |
| Note clearing — send empty field, don't omit it | `ihme edit foo --note ""` is a no-op today |
| `jq` presence check with install hint | "exec: jq: not found" is not actionable |
| CI lint — bump golangci-lint for go1.26 | CI red since v0.1.0, unrelated to code quality |
| `forward set` basic email validation | Server rejects garbage but with a worse message |
| Generate dupe warning when fewer candidates returned | Silent short-count surprises scripts |

## v0.4 — Session resilience (what v0.2.0 didn't cover)

| Item | Why |
|------|-----|
| Lock file for concurrent access | Parallel agent invocations corrupt session.json |

## v0.5 — Agent-native features

| Item | Why |
|------|-----|
| `ihme audit` — surface old addresses, inactive services, missing tags | 326 addresses, 1 deactivated. Hygiene at scale |
| `ihme bulk deactivate --tag throwaway --older-than 6m` | Batch lifecycle for agent-driven cleanup |
| Structured stderr (JSON errors when `--json` is set) | Agents can't parse human error messages reliably |
| Shell completions with dynamic ref completion | Tab-complete labels and IDs for humans |

## v0.6 — Distribution

| Item | Why |
|------|-----|
| Homebrew tap (`brew install lroolle/tap/ihme`) | Primary install path for macOS |
| AUR package | Linux distribution |
| README demo GIF (asciinema) | The README sells; the code ships |
| Issue templates (bug, feature) | Signal maturity, structure contributions |

## v1.0 — Production grade

| Item | Why |
|------|-----|
| `api/` test coverage 16.9% -> 60%+ via recorded HTTP fixtures | Auth flow is the hardest code and least tested |
| API change detection — weekly CI probe against Apple | Undocumented API = silent breakage |
| Keyring integration (macOS Keychain, GNOME Keyring) | Optional alternative to plaintext session JSON |
| Stable JSON output contract (versioned, no breaking changes) | Agents and scripts depend on response shapes |
| Man pages from Cobra | Unix convention |

## Strategic position

Only open-source CLI for iCloud Hide My Email that works end-to-end. pyicloud doesn't do HME. rclone does storage. Go-iClient is a library.

The moat is operational knowledge of Apple's undocumented auth protocol — SRP-6a with custom modifications, mandatory 2FA, trust tokens, CN routing. This breaks without warning and requires re-reverse-engineering.

Optimize for:
1. **Resilience** — sessions that survive, retries that work, change detection
2. **Agent ergonomics** — structured output, taste scoring, bulk ops
3. **Distribution** — people can't use what they can't install
