# ihme

iCloud Hide My Email from the terminal. One address per signup. Find it later. Deactivate when done.

```bash
ihme new github.com          # pick from Apple's candidates
ihme list --search github    # by label, address, or note
ihme deactivate              # no address: pick one, newest first
ihme share netflix           # grant read of that address
ihme export -o backup.csv    # your own copy
```

Needs iCloud+ and an Apple ID with 2FA. Unofficial: ihme uses the same web API as icloud.com. Apple can change it or block it.

[中文](README.zh.md)

## Install

macOS on Apple Silicon:

```bash
curl -fL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_arm64.tar.gz -o ihme.tar.gz
tar -xzf ihme.tar.gz ihme
sudo install -m 755 ihme /usr/local/bin/ihme
```

macOS Intel, Linux, Windows (arm64, x86-64): [releases](https://github.com/lroolle/ihme-cli/releases/latest), with SHA-256 checksums. Go: `go install github.com/lroolle/ihme-cli/cmd/ihme@latest`.

```bash
ihme auth login    # Apple ID, password, 2FA. Saves session, not password or code.
ihme list
```

## Scripts

Every address command takes `--json`. `--jq` runs installed jq on it. No prompts without a terminal.

```bash
ihme new github.com --json                        # candidates only, reserves nothing
ihme new github.com --address <candidate> --json  # reserve one
ihme new github.com --yes --json                  # reserve the first
ihme list --json --jq '.addresses[].hme'
```

Exit 2: sign in again. Exit 1: anything else.

## Share one inbox

```bash
ihme share netflix    # prints a key and a link, once
ihme serve            # http://127.0.0.1:8025
```

The key opens one address's mail: INBOX and Junk, last 30 days, as plain text. No other address, no other recipients, nothing loaded from anywhere else. The server reads mail over IMAP with an app-specific password, never your iCloud session. `ihme share revoke netflix` ends it. [Details](docs/usage.md#share-an-inbox).

## Agents

- `ihme agent "find my github address"`: built-in assistant on your model key (Anthropic, DeepSeek, any OpenAI-compatible endpoint).
- `ihme agent --via codex "find my github address"`: a coding agent you are already signed in to. Also `claude`, `opencode`. No API key.
- `npx skills add lroolle/ihme-cli -g`: the [skill](skill/SKILL.md) for an outside agent. `ihme mcp`: a stdio MCP server.

Account changes ask first. Preferences live in plain Markdown; `ihme memory` shows them. An agent run sends the task and results to the model you picked. Plain commands never talk to a model.

## Docs

- [Commands](docs/usage.md): search, create, edit, tags, share, export, JSON, errors
- [Agents](docs/agents.md): providers, consent, memory, MCP
- [Roadmap and release notes](ROADMAP.md)
- [Security](SECURITY.md): what is stored, and where
- [Contributing](CONTRIBUTING.md)

## Development

```bash
make check    # vet, test, build
make site     # website into _site/
```

Built with [Cobra](https://github.com/spf13/cobra) and [Charm](https://charm.sh/). [MIT](LICENSE). Not affiliated with Apple.
