<div align="center">

# ihme

**iCloud Hide My Email, from your terminal.**

Create an alias for a signup. Find it later. Turn it off when the mail gets noisy.

[![Release](https://img.shields.io/github/v/release/lroolle/ihme-cli)](https://github.com/lroolle/ihme-cli/releases/latest)
[![CI](https://github.com/lroolle/ihme-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/lroolle/ihme-cli/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[Website](https://lroolle.github.io/ihme-cli/) · [Install](#install) · [Proof](#proof) · [Commands](docs/usage.md) · [Agents](docs/agents.md)

<sub>For agents: start with <a href="https://lroolle.github.io/ihme-cli/llms.txt">llms.txt</a>.</sub>

</div>

```bash
ihme new github.com                   # pick an address in your terminal
ihme list --search github             # find it by label, address, or note
ihme export -o backup.csv             # keep a copy of your aliases
```

Use it by hand, pipe JSON into a script, or ask an agent to manage your aliases. The built-in assistant can use your model key or a coding agent you already sign in to. Normal CLI commands need neither.

Requires an **iCloud+ subscription** and interactive Apple ID sign-in with 2FA. This is an unofficial client of Apple's undocumented web API; Apple can change or block access.

## Proof

- **Published binaries:** [Releases](https://github.com/lroolle/ihme-cli/releases/latest) include macOS, Linux, and Windows builds for ARM64 and x86-64, plus SHA-256 checksums.
- **Executable checks:** [CI](https://github.com/lroolle/ihme-cli/actions/workflows/ci.yml) runs lint, vet, tests, and a build. Tests exercise SRP authentication, session recovery, address operations, filtering, and agent consent using local fixtures and mock servers.
- **Reproduce locally:** clone the repo, install the Go version in [go.mod](go.mod), and run `make check`. Use `make test-cover` to measure coverage yourself.

These checks validate the code against fixtures. They do not prove Apple's live service is available or that a particular account can sign in.

## Install

Download a binary from [the latest release](https://github.com/lroolle/ihme-cli/releases/latest). For macOS with Apple Silicon:

```bash
curl -fL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_arm64.tar.gz -o ihme.tar.gz
tar -xzf ihme.tar.gz ihme
sudo install -m 755 ihme /usr/local/bin/ihme
ihme version
```

<details>
<summary>Other platforms and Go install</summary>

Use the same steps with the archive matching your system. Windows downloads are ZIP files; extract `ihme.exe` into a directory on your `PATH`.

| Platform | Release asset |
| --- | --- |
| macOS Apple Silicon | `ihme_macOS_arm64.tar.gz` |
| macOS Intel | `ihme_macOS_x86_64.tar.gz` |
| Linux ARM64 | `ihme_linux_arm64.tar.gz` |
| Linux x86-64 | `ihme_linux_x86_64.tar.gz` |
| Windows ARM64 | `ihme_windows_arm64.zip` |
| Windows x86-64 | `ihme_windows_x86_64.zip` |

With Go installed:

```bash
go install github.com/lroolle/ihme-cli/cmd/ihme@latest
ihme version
```

The binary lands in `GOBIN`, or `$(go env GOPATH)/bin` when `GOBIN` is unset. Add that directory to `PATH` if needed.

</details>

## Quick start

```bash
ihme auth login                      # interactive Apple ID + 2FA
ihme auth status --json               # verify your saved session
ihme list                            # see your aliases
ihme new github.com                  # choose and reserve an alias
```

Sign-in time depends on Apple's verification flow. `new` creates a real alias. In a terminal it asks you to choose; use `ihme new github.com --json` to generate candidates without reserving one.

## Pick, script, or ask

| How you work | Command | What happens |
| --- | --- | --- |
| Pick it yourself | `ihme new github.com` | In a terminal, choose from the returned candidates |
| Generate first | `ihme new github.com --json` | Get candidates without reserving; reserve with `--address <candidate>` |
| Automate | `ihme new github.com --yes --json` | Reserve the first candidate |
| Ask an agent | `ihme agent --via codex "find my github address"` | Run a task through your installed, signed-in coding agent |

Address commands expose `--json` for scripts. Filtering with `--jq` requires `jq` on your `PATH`:

```bash
ihme list --search github --json --jq '.addresses[].hme'
```

The [agent guide](docs/agents.md) covers Claude, Codex, OpenCode, model keys, consent, and memory. To install instructions for an external agent, run `npx skills add lroolle/ihme-cli -g`; install and authenticate the binary separately.

## When it fits

Use ihme if you already use Hide My Email and want searchable labels, CSV/JSON exports, or an alias workflow in your terminal. Use [Apple's settings or iCloud.com](https://support.apple.com/guide/icloud/what-you-can-do-with-icloud-and-hide-my-email-mme38e1602db/icloud) if you only need an occasional alias and prefer Apple's supported interface.

Skip it if you need an official API, bulk address creation, or an email service independent of iCloud+. Apple controls candidate pools and rate limits. Agent mode also sends task context and tool results to your selected model provider; use direct commands if you do not want that.

## Reference

| Start here | What it covers |
| --- | --- |
| [Command guide](docs/usage.md) | Search, create, edit, tags, lifecycle, exports, JSON, and errors |
| [Agent guide](docs/agents.md) | Providers, coding agents, consent, preferences, memory, and MCP |
| [Agent skill](skill/SKILL.md) | The address-selection procedure and taste rubric |
| [Release history and roadmap](ROADMAP.md) | Shipped behavior and planned work |
| [Security](SECURITY.md) | Session storage, model data, and vulnerability reports |
| [Agent kernel](pkg/agentkit/README.md) | The reusable Go library behind the assistant |

## Development

```bash
make check         # vet, tests, and build
make test-cover    # local coverage report
make site          # build and check the static Pages site (Python 3)
```

Bug reports should include `ihme version`, the command, and a redacted error. See [Contributing](CONTRIBUTING.md) for checks and [Security](SECURITY.md) for private reports.

Built with [Cobra](https://github.com/spf13/cobra) and [Charm](https://charm.sh/). Agent handoffs use the [Agent Client Protocol](https://agentclientprotocol.com). Tags follow the convention used by the [Hide My Email browser extension](https://github.com/dedoussis/icloud-hide-my-email-browser-extension).

[MIT](LICENSE).
