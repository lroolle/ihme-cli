# ihme

Manage iCloud Hide My Email addresses from the terminal. List, create, search, edit, deactivate, export — for humans and AI agents.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> **Disclaimer**: Unofficial tool using Apple's undocumented iCloud web API. Use at your own risk. Not affiliated with Apple.

## Install

**Go install:**
```bash
go install github.com/lroolle/ihme-cli/cmd/ihme@latest
```

**Build from source:**
```bash
git clone https://github.com/lroolle/ihme-cli.git
cd ihme-cli
make install
```

**Install via Claude Code** — paste this:
```
Install the ihme CLI from github.com/lroolle/ihme-cli using go install,
then copy skill/SKILL.md to ~/.claude/skills/ihme-cli/SKILL.md so I can
manage iCloud Hide My Email addresses.
```

## Quick start

```bash
# Sign in (Apple ID + 2FA)
ihme auth login

# List all addresses
ihme list

# Search
ihme list --search netflix

# Create a new address (interactive: shows 3 candidates, pick one)
ihme new github.com

# View details
ihme view github.com

# Export
ihme export -o addresses.csv
```

## Commands

```
ihme auth login              Sign in with Apple ID (SRP + 2FA)
ihme auth status             Show session state
ihme auth logout             Clear session

ihme list                    List all addresses
ihme list --search <query>   Search label, address, or note
ihme list --active           Filter by status
ihme list --tag <tag>        Filter by tag
ihme list --sort label       Sort by label or date

ihme new <label>             Generate candidates, pick, reserve
ihme new <label> --yes       Auto-pick first candidate
ihme new <label> --json      Get candidates for agent selection

ihme view <ref>              View address details
ihme edit <ref>              Edit label, note, tags
ihme copy <ref>              Copy address to clipboard
ihme deactivate <ref>        Stop receiving mail
ihme reactivate <ref>        Resume receiving mail
ihme delete <ref>            Permanently delete (interactive confirm)

ihme export                  Export to CSV (default) or JSON
ihme forward                 Show/change forward-to address
```

`<ref>` accepts an anonymousId, email address, or label (fuzzy match).

## JSON output

Every command supports `--json` and `--jq`. Response shapes are documented in `ihme <cmd> --help`.

```bash
# List with jq
ihme list --json --jq '.addresses[0:5]'
ihme list --search github --json --jq '.addresses[].hme'

# View a single address
ihme view github.com --json --jq '.result.hme'

# Create: get candidates, then reserve
candidates=$(ihme new github.com --json)
ihme new github.com --address $(echo $candidates | jq -r '.candidates[0]') --json

# One-shot create
ihme new github.com --yes --json
```

## Creating addresses

The `ihme new` flow matches iCloud's web UI:

| Mode | Command | Behavior |
|------|---------|----------|
| Human | `ihme new github.com` | Show ~3 candidates, pick interactively |
| Agent | `ihme new github.com --json` | Return candidates, reserve with `--address` |
| Script | `ihme new github.com -y` | Take first candidate, reserve immediately |

Apple's pool rotates ~3 unique addresses. The CLI deduplicates and stops early when the pool is exhausted.

## Tags

Tags stored in the note field using `#tag | note` convention:

```bash
ihme new example.com --tag shopping --note "prime account"
# Stored as: #shopping | prime account

ihme list --tag shopping
ihme edit example.com --tag shopping,personal
```

Compatible with the [icloud-hide-my-email-browser-extension](https://github.com/dedoussis/icloud-hide-my-email-browser-extension) tag format.

## Agent integration

Designed for AI agents (Claude Code, etc.):

- **JSON schemas in `--help`**: every command documents its response shape
- **Hints in JSON output**: next-action commands included in responses
- **Actionable errors**: wrong usage returns the correct usage and an example
- **`--yes` flag**: skip interactive prompts for scripted use
- **Deterministic exit codes**: 0 success, 1 error, 2 auth required

Ships with a Claude Code skill definition in [`skill/SKILL.md`](skill/SKILL.md).

## Auth

SRP-6a authentication over `idmsa.apple.com`:

1. SRP handshake (password never transmitted)
2. Two-factor authentication (SMS or trusted device push)
3. Trust token stored locally (~30 day validity)

Session file locations:
- macOS: `~/Library/Application Support/ihme/session.json`
- Linux: `~/.config/ihme/session.json`

Override with `IHME_SESSION_PATH`. File permissions: `0600`.

Credentials are never stored. Only session tokens and cookies are persisted.

## Limits

- ~5 addresses per 30 minutes (Apple rate limit)
- ~750 total addresses per account
- Trust token valid ~30 days before re-authentication

## Development

```bash
make              # build
make test         # run tests
make test-cover   # tests with coverage
make check        # vet + test + build
make cross        # build for linux/darwin/windows
make completions  # generate shell completions
```

## License

[MIT](LICENSE)
