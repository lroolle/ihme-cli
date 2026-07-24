<p align="center">
  <h1 align="center">ihme</h1>
  <p align="center">
    iCloud Hide My Email, from the terminal.<br>
    For humans who pick. For agents who script. For scripts that just run.
  </p>
</p>

<p align="center">
  <a href="https://github.com/lroolle/ihme-cli/releases"><img src="https://img.shields.io/github/v/release/lroolle/ihme-cli?color=blue&label=release" alt="Release"></a>
  <a href="https://github.com/lroolle/ihme-cli/actions/workflows/ci.yml"><img src="https://github.com/lroolle/ihme-cli/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="https://github.com/lroolle/ihme-cli/actions/workflows/ci.yml"><img src="https://img.shields.io/badge/coverage-SRP%2097%25%20|%20filter%20100%25-brightgreen" alt="Coverage"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="MIT License"></a>
</p>

<p align="center">
  <img src="docs/demo.svg" alt="ihme demo" width="680">
</p>

<div align="center">

[Install](#install) · [Quick start](#quick-start) · [Commands](#all-commands) · [Agent integration](#agent-integration) · [Auth](#auth-details) · [Roadmap](ROADMAP.md)

</div>

---

> **Note**: Unofficial tool. Uses Apple's undocumented iCloud web API. Not affiliated with Apple. Use at your own risk.

## Install

**A. Download binary** from [Releases](https://github.com/lroolle/ihme-cli/releases):

```bash
# macOS (Apple Silicon)
curl -sL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_arm64.tar.gz | tar xz
sudo mv ihme /usr/local/bin/

# Linux
curl -sL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_linux_x86_64.tar.gz | tar xz
sudo mv ihme /usr/local/bin/
```

**B. Go install:**

```bash
go install github.com/lroolle/ihme-cli/cmd/ihme@latest
```

**C. With [`skills`](https://github.com/vercel-labs/skills) (any compatible agent):**

```bash
npx skills add lroolle/ihme-cli -g
```

The `-g` flag installs globally so every project picks it up.

**D. Or paste this prompt to your AI agent:**

```
Install the ihme skill for iCloud Hide My Email management:

1. Download and install the binary:
   curl -sL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_arm64.tar.gz | tar xz && sudo mv ihme /usr/local/bin/
2. Install the skill definition:
   mkdir -p ~/.claude/skills/ihme-cli && curl -sL https://raw.githubusercontent.com/lroolle/ihme-cli/main/skill/SKILL.md -o ~/.claude/skills/ihme-cli/SKILL.md
3. Verify: ihme version
4. Authenticate: ihme auth login (interactive — needs Apple ID + 2FA)
```

## Quick start

```bash
ihme auth login                    # Sign in (Apple ID + 2FA)
ihme list                          # See all your addresses
ihme list --search netflix         # Find one
ihme new github.com                # Create (pick from 3 candidates)
ihme view github.com               # Details
ihme export -o backup.csv          # Export everything
```

## How `ihme new` works

Matches the iCloud web flow — generate candidates, pick the one you like, reserve it:

```
$ ihme new github.com --tag dev

  [1] uploads_tease.6t@icloud.com
  [2] copay.jacket-4c@icloud.com
  [3] rotors.gutless.7q@icloud.com

Select [1-3] or [c]ancel: 2
Reserved: copay.jacket-4c@icloud.com (label: github.com)
```

| Who | Command | What happens |
|-----|---------|-------------|
| Human | `ihme new github.com` | Show ~3 candidates, pick interactively |
| Built-in agent | `ihme new github.com --agent` | The embedded agent runs the whole procedure: search, generate, taste-test, reserve — and tells you why it picked |
| Agent | `ihme new github.com --json` then `--address <pick>` | Get candidates, reserve one |
| Script | `ihme new github.com -y` | Take first, reserve, done |

## All commands

```
AUTH
  ihme auth login                Sign in with Apple ID (SRP + 2FA)
  ihme auth status [--json]      Session state
  ihme auth logout               Clear session

LIST & SEARCH
  ihme list                      All addresses (table)
  ihme list --search <query>     Search label, address, or note
  ihme list --active             Only active
  ihme list --tag <tag>          Filter by tag
  ihme list --sort label         Sort by label or date

CREATE
  ihme new <label>               Interactive: pick from ~3 candidates
  ihme new <label> --yes         Script: take first
  ihme new <label> --json        Agent: get candidates without reserving
  ihme new <label> --agent       Embedded agent runs the full procedure

AGENT (built-in, BYOK)
  ihme agent                     Interactive session (inline TUI)
  ihme agent "<task>"            One-shot: "deactivate my old figma alias"
  ihme agent -p "<task>"         Same, via --prompt/-p (also: ihme -p "<task>")
  ihme agent --grant auto        Skip consent prompts for this run
  ihme memory                    Inspect the agent's memory graph
  ihme memory search <query>     Search journals and pages
  ihme memory graph              Show topic pages and backlinks

MANAGE
  ihme view <ref>                View details
  ihme edit <ref>                Edit label, note, tags
  ihme copy <ref>                Copy address to clipboard
  ihme deactivate <ref>          Stop receiving mail
  ihme reactivate <ref>          Resume receiving mail
  ihme delete <ref> [--yes]      Permanent deletion (confirms first)

EXPORT
  ihme export                    CSV to stdout
  ihme export --format json      JSON to stdout
  ihme export -o file.csv        To file
  ihme export --search dev       Filtered export

FORWARD
  ihme forward [--json]          Show forward-to address
  ihme forward set <email>       Change it
```

`<ref>` resolves by: anonymousId (full or 6+ char prefix) > email > label (exact) > label (fuzzy).

## JSON & jq

Every command supports `--json` and `--jq`. Response shapes are documented in `ihme <cmd> --help`.

```bash
ihme list --json --jq '.addresses[0:5]'
ihme list --search github --json --jq '.addresses[].hme'
ihme list --json --jq '.count'
ihme view github.com --json --jq '.result.hme'
ihme new mysite.com --json | jq '.candidates'
```

## Built-in agent

`ihme` ships with an embedded assistant — bring your own model key, and it runs the address workflow end to end:

```bash
# one-time config (any OpenAI-compatible endpoint)
mkdir -p ~/.config/ihme && cat > ~/.config/ihme/.env <<'ENV'
OPENAI_API_KEY=sk-...
OPENAI_MODEL=gpt-5-mini        # or any /chat/completions | /v1/responses model
ENV

ihme new "github signup" --agent    # scoped: may reserve ONE address for this label
ihme agent                          # interactive session (inline TUI)
ihme agent "tag my linear address as work"   # one-shot task
ihme --agent -p "new for github signup"      # one-shot via --prompt/-p
```

Every run opens by stating the effective configuration — `Model:` and
`Thinking effort:` as actually resolved from config, environment, and
flags (effort is a responses-API parameter; on chat-completions models
it is reported as not applicable rather than echoed).

What you get, and what it must earn:

- **The verdict is spoken.** Reserving requires the winner's taste rationale plus one entry per rejected candidate with its failure. The consent card shows all of it — the address, the label/note/tags it will write, the why, and what it passed on — and it lands in the `✓ reserved` banner and `--json` output (`rationale`, `rejected`); the reserved address lands on your clipboard (macOS/Linux).
- **Consent is a conversation.** `ihme new <label> --agent` pre-grants exactly one reservation for that label; `ihme agent` pre-grants *nothing* — every mutation asks: allow once, deny, always this run, **or type a reply** ("use the calm one, tag it work") and the agent adapts and re-asks. `--grant auto` opts out per run.
- **Visible thinking.** Reasoning summaries stream live in the status line; hard limits on generation rounds and tool calls are enforced in code, not in the prompt.
- **It refreshes a stale pool** *(experimental)*. Apple hands out a small pool of generated addresses that repeats until one is consumed, so when nothing passes taste and re-generating returns the same options, the agent can burn a throwaway — reserve, deactivate, delete — to force a fresh pool. Bounded (2 per task) and net-zero on the common path. The reserve-to-refresh behavior is pending real-world validation; it degrades to a plain re-generate if Apple doesn't cooperate.
- **It remembers — visibly.** The agent keeps a memory across runs — a plain [Logseq](https://logseq.com)-style markdown graph it finds on its own (`$IHME_MEMORY_PATH` to relocate). Reservations journal themselves, each topic page accumulates a service's history, and a `flashcards` page loads into every run. Every memory operation states what actually happened — `Memory created for "x"`, `Memory updated for "x"`, `Reused memory for "x"` — and a failed write says so instead of pretending. Inspect it with `ihme memory` (`search`, `graph`, `card`, `path`), or open the directory in Logseq or Obsidian — no database, just files.
- **Wire protocol is auto-detected** per model family (`gpt-5*`/`o1`/`o3`/`codex` → responses API, else chat completions), corrected on the endpoint's misroute signal, and persisted to `~/.config/ihme/agent.json`.

The agent kernel is [`pkg/agentkit`](pkg/agentkit/README.md) — stdlib-only, embeddable, reusable outside this CLI.

## Agent integration

Built for AI agents — Claude Code, Codex, Cursor, Gemini CLI, Copilot, Windsurf, Devin, Amp, Junie, and any tool that speaks [agents.md](https://agents.md):

| Feature | How |
|---------|-----|
| **Response schemas** | Documented in every `--help` |
| **Next-action hints** | JSON output includes `hints` with follow-up commands |
| **Actionable errors** | Wrong usage returns the fix: `Usage: ihme view <ref>` |
| **No prompts** | `--yes` skips all interactive confirmation |
| **Exit codes** | 0 success, 1 error, 2 auth required |
| **Composable** | stdout = data, stderr = status |

Ships with a Claude Code skill ([`skill/SKILL.md`](skill/SKILL.md)) and an [`AGENTS.md`](AGENTS.md) for compatible agents.

## Tags

Shared convention with the [browser extension](https://github.com/dedoussis/icloud-hide-my-email-browser-extension):

```bash
ihme new example.com --tag shopping --note "prime account"
# Stored as: #shopping | prime account

ihme list --tag shopping
```

## Auth details

SRP-6a over `idmsa.apple.com`. Password never transmitted.

- 2FA: SMS or trusted device push (iOS 26.4+ supported)
- Trust token: ~30 days, skips 2FA on subsequent logins
- Session: `~/.config/ihme/session.json` (respects `$XDG_CONFIG_HOME`)
- Credentials never stored. File permissions `0600`.
- Override path: `IHME_SESSION_PATH`

## Limits

| Limit | Value |
|-------|-------|
| Addresses per 30 min | ~5 |
| Total per account | ~750 |
| Trust token lifetime | ~30 days |
| Candidate pool | ~3 unique |

## Development

```bash
make              # build
make test         # tests
make test-cover   # with coverage
make check        # vet + test + build
make cross        # linux/darwin/windows x amd64/arm64
make completions  # bash/zsh/fish
```

## License

[MIT](LICENSE)
