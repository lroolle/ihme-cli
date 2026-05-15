# ihme

A CLI for iCloud Hide My Email. Manage email aliases from the terminal.

Requires an iCloud+ subscription.

> **Disclaimer**: This is an unofficial tool. It uses Apple's undocumented iCloud web API. Use at your own risk. Apple may change or block access at any time.

## Install

```bash
go install github.com/lroolle/ihme-cli/cmd/ihme@latest
```

Or build from source:

```bash
git clone https://github.com/lroolle/ihme-cli.git
cd ihme-cli
make install
```

## Quick start

```bash
ihme auth login
ihme list
ihme new github.com
ihme view github.com
ihme list --search netflix
```

## Commands

| Command | Description |
|---------|-------------|
| `auth login` | Sign in with Apple ID (SRP + 2FA) |
| `auth status` | Show current session state |
| `auth logout` | Clear stored session |
| `list` | List addresses with filters |
| `new <label>` | Generate candidates, pick, reserve |
| `view <ref>` | View address details |
| `edit <ref>` | Edit label, note, or tags |
| `copy <ref>` | Copy address to clipboard |
| `deactivate <ref>` | Stop receiving mail |
| `reactivate <ref>` | Resume receiving mail |
| `delete <ref>` | Permanently delete |
| `export` | Export to CSV or JSON |
| `forward` | Show/change forward-to address |

`<ref>` accepts an anonymousId, email address, or label (fuzzy match).

## Search and filter

```bash
ihme list --search netflix
ihme list --active --tag dev
ihme list --sort label
ihme list --search github --active --json
```

## Creating addresses

Interactive (human):
```bash
$ ihme new github.com
  [1] abc123@icloud.com
  [2] xyz789@icloud.com
  [3] def456@icloud.com
Select [1-3]: 2
Reserved: xyz789@icloud.com (label: github.com)
```

Agent workflow:
```bash
$ ihme new github.com --json
{"candidates":["abc@icloud.com","def@icloud.com","ghi@icloud.com"],...}
$ ihme new github.com --address def@icloud.com --json
```

Script:
```bash
ihme new github.com --yes --json
```

## JSON output

Every command supports `--json` and `--jq`. Response schemas are documented in each command's `--help`:

```bash
ihme list --json --jq '.addresses[0:5]'
ihme view github.com --json --jq '.result.hme'
ihme auth status --json
```

## Tags

Tags stored in the note field using `#tag` convention:

```bash
ihme new example.com --tag shopping --note "prime account"
ihme list --tag shopping
ihme edit example.com --tag shopping,personal
```

Compatible with the [icloud-hide-my-email-browser-extension](https://github.com/dedoussis/icloud-hide-my-email-browser-extension) tag format.

## Auth

SRP-6a authentication over `idmsa.apple.com`. Credentials are never stored. Session tokens saved to:

- macOS: `~/Library/Application Support/ihme/session.json`
- Linux: `~/.config/ihme/session.json`

Trust token valid ~30 days. Override path with `IHME_SESSION_PATH`.

## Agent integration

Designed for AI agents (Claude Code, etc.):

- `--json` on every command with documented response schemas
- `--jq` for inline filtering
- `--yes` on destructive commands to skip confirmation
- Actionable error messages with fix commands
- `--help` includes JSON output shapes for each command
- Hints in JSON output point to the next useful command

Ships with a Claude Code skill definition in `skill/SKILL.md`.

## Limits

- ~5 addresses per 30 minutes (Apple rate limit)
- ~750 total addresses per account
- Trust token valid ~30 days before re-authentication
- Apple's pool rotates ~3 candidate addresses

## Disclaimer

This project is not affiliated with Apple. It uses Apple's undocumented iCloud web API, which may change without notice. The authors are not responsible for any consequences of using this tool, including but not limited to account restrictions.

## License

MIT
