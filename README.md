# ihme

A CLI for iCloud Hide My Email. Manage email aliases from the terminal.

Requires an iCloud+ subscription.

## Install

```bash
go install github.com/lroolle/ihme-cli/cmd/ihme@latest
```

Or build from source:

```bash
git clone https://github.com/lroolle/ihme-cli.git
cd ihme-cli
go build -o ihme ./cmd/ihme
```

## Quick start

```bash
# Sign in (Apple ID + 2FA)
ihme auth login

# List all addresses
ihme list

# Create a new address
ihme new github.com --tag dev --note "main account"

# View details
ihme view github.com

# Copy to clipboard
ihme copy github.com

# Deactivate
ihme deactivate github.com

# Export
ihme export -o addresses.csv
```

## Commands

| Command | Description |
|---------|-------------|
| `auth login` | Sign in with Apple ID (SRP auth + 2FA) |
| `auth status` | Show current session state |
| `auth logout` | Clear stored session |
| `list` | List addresses with `--active`, `--inactive`, `--tag` filters |
| `new <label>` | Generate and reserve a new address |
| `view <ref>` | View address details |
| `edit <ref>` | Edit label, note, or tags |
| `copy <ref>` | Copy address to clipboard |
| `deactivate <ref>` | Stop receiving mail |
| `reactivate <ref>` | Resume receiving mail |
| `delete <ref>` | Permanently delete (interactive confirmation) |
| `export` | Export to CSV or JSON |
| `forward` | Show/change forward-to address |

`<ref>` accepts an anonymousId, email address, or label (fuzzy match).

## JSON output

Every command supports `--json` and `--jq`:

```bash
# All addresses as JSON
ihme list --json

# Just the email addresses
ihme list --json --jq '.[].hme'

# Inactive addresses older than 2024
ihme list --json --jq '[.[] | select(.isActive == false and .createTimestamp < 1704067200000)]'
```

## Tags

Tags are stored in the note field using the `#tag` convention:

```bash
ihme new example.com --tag shopping --note "prime account"
# Stored as: #shopping | prime account

ihme list --tag shopping
ihme edit example.com --tag shopping,personal
```

Compatible with the [icloud-hide-my-email-browser-extension](https://github.com/dedoussis/icloud-hide-my-email-browser-extension) tag format.

## Auth

Authentication uses Apple's SRP-6a protocol over `idmsa.apple.com`:

1. SRP handshake (no password transmitted)
2. Two-factor authentication (trusted device push)
3. Trust token stored locally (~30 day validity)

Session is saved at `~/.config/ihme/session.json` with `0600` permissions. Set `IHME_SESSION_PATH` to override. Set `IHME_APPLE_ID` to skip the interactive prompt.

## Agent integration

ihme is designed for use by AI agents (Claude Code, etc.):

- `--json` on every command for structured output
- `--jq` for inline filtering
- `--yes` flag on destructive commands to skip confirmation
- Deterministic exit codes: 0 success, 1 error, 2 auth required
- All output to stdout, errors to stderr

Ships with a Claude Code skill definition in `skill/SKILL.md`.

## Limits

- ~5 addresses per 30 minutes (Apple rate limit)
- ~750 total addresses per account
- Trust token valid ~30 days before re-authentication

## License

MIT
