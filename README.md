# ihme

iCloud Hide My Email from the terminal. Make an address for a signup, find it later, turn it off when you are done with it.

```bash
ihme new github.com          # Apple offers a few addresses, you pick one
ihme list --search github    # find it by label, address, or note
ihme deactivate github.com   # stop the mail
ihme export -o backup.csv    # keep your own copy
```

You need iCloud+ and an Apple ID with two-factor authentication. Apple has no public API for this. ihme uses the same web API as icloud.com, and Apple can change it or block it. That is the deal.

[中文说明](README.zh.md)

## Install

macOS on Apple Silicon:

```bash
curl -fL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_arm64.tar.gz -o ihme.tar.gz
tar -xzf ihme.tar.gz ihme
sudo install -m 755 ihme /usr/local/bin/ihme
```

Builds for macOS Intel, Linux, and Windows, on arm64 and x86-64, are on the [releases page](https://github.com/lroolle/ihme-cli/releases/latest) with SHA-256 checksums. With Go: `go install github.com/lroolle/ihme-cli/cmd/ihme@latest`.

Then sign in:

```bash
ihme auth login    # Apple ID, password, 2FA code. The session is saved, the password and code are not.
ihme list
```

## Scripts

Every address command takes `--json`. `--jq` runs an installed jq on the result.

```bash
ihme new github.com --json                        # candidates only, reserves nothing
ihme new github.com --address <candidate> --json  # reserve one of them
ihme new github.com --yes --json                  # reserve the first one
ihme list --json --jq '.addresses[].hme'
```

Exit code 2 means sign in again. 1 is everything else.

## Agents

ihme is also a tool for agents. Three ways:

- `ihme agent "find my github address"` runs the built-in assistant on your own model key: Anthropic, DeepSeek, or any OpenAI-compatible endpoint.
- `ihme agent --via codex "find my github address"` uses a coding agent you are already signed in to. Also `claude` and `opencode`. No API key.
- `npx skills add lroolle/ihme-cli -g` gives an outside agent the [skill](skill/SKILL.md). `ihme mcp` is a stdio MCP server.

Anything that changes your account asks you first. The assistant keeps preferences in plain Markdown files. `ihme memory` shows them. An agent run sends the task and the results to the model you picked. Plain commands never talk to a model.

## Docs

- [Commands](docs/usage.md): search, create, edit, tags, export, JSON, errors
- [Agents](docs/agents.md): providers, consent, memory, MCP
- [Roadmap and release notes](ROADMAP.md)
- [Security](SECURITY.md): what is stored, and where
- [Contributing](CONTRIBUTING.md)

## Development

```bash
make check    # vet, test, build
make site     # build the website into _site/
```

Built with [Cobra](https://github.com/spf13/cobra) and [Charm](https://charm.sh/). [MIT](LICENSE). Not affiliated with Apple.
