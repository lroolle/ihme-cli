# AGENTS.md

> Coding agent instructions for [ihme-cli](https://github.com/lroolle/ihme-cli) — iCloud Hide My Email from the terminal.

## What this tool does

`ihme` manages iCloud Hide My Email addresses: create, list, search, edit, deactivate, reactivate, delete, export. It authenticates via Apple's SRP-6a protocol with 2FA support and persists session tokens locally.

## Prerequisites

Binary must be installed and authenticated before use:

```bash
# Install (macOS ARM)
curl -sL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_arm64.tar.gz | tar xz && sudo mv ihme /usr/local/bin/

# Install (macOS Intel)
curl -sL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_x86_64.tar.gz | tar xz && sudo mv ihme /usr/local/bin/

# Install (Linux x86_64)
curl -sL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_linux_x86_64.tar.gz | tar xz && sudo mv ihme /usr/local/bin/

# Install (Go)
go install github.com/lroolle/ihme-cli/cmd/ihme@latest
```

Authentication is interactive (requires human input for Apple ID + 2FA):

```bash
ihme auth login
```

Check session before running commands:

```bash
ihme auth status --json
# {"loggedIn":true,"appleId":"...","expired":false,...}
```

If exit code is 2, the user must run `ihme auth login` interactively.

## Commands

All commands support `--json` for structured output and `--jq <expr>` for filtering.

### List and search

```bash
ihme list --json                          # all addresses
ihme list --search netflix --json         # search label, address, or note
ihme list --active --json                 # only active
ihme list --inactive --json               # only inactive
ihme list --tag dev --json                # filter by tag
ihme list --sort label --json             # sort by: label, date, label:desc, date:asc
ihme list --json --jq '.addresses[0:5]'   # first 5
ihme list --json --jq '.count'            # total count
```

### Create (two-step flow)

```bash
# Step 1: generate candidates (does NOT reserve)
ihme new github.com --json
# {"candidates":["a@icloud.com","b@icloud.com","c@icloud.com"],"hint":"ihme new github.com --address <address> --json","label":"github.com"}

# Step 2: reserve a specific candidate
ihme new github.com --address a@icloud.com --json
# {anonymousId, label, hme, isActive, ...}

# One-shot (take first candidate, no choice)
ihme new github.com --yes --json

# With tags and note
ihme new github.com --tag dev --note "main account" --json
```

### View, edit, copy

```bash
ihme view github.com --json
ihme edit github.com --label GitHub --tag dev,work --json
ihme copy github.com                      # outputs address to stdout
```

### Lifecycle

```bash
ihme deactivate github.com --json         # stop receiving mail
ihme reactivate github.com --json         # resume receiving mail
ihme delete github.com --yes --json       # permanent deletion
```

### Export

```bash
ihme export --format json                 # JSON to stdout
ihme export -o backup.csv                 # CSV to file
ihme export --search dev --active         # filtered export
```

### Forward-to address

```bash
ihme forward --json                       # show current forward-to
ihme forward set user@icloud.com          # change it
```

## Reference resolution

The `<ref>` argument resolves in this order:

1. Exact anonymousId match
2. anonymousId prefix (>= 6 chars, unique match only)
3. Exact email match
4. Exact label match (case-insensitive)
5. Fuzzy label substring match (single match only)

Ambiguous matches return an error listing the candidates.

## JSON response shapes

```
list --json      {"addresses":[{anonymousId,label,hme,isActive,createTimestamp,note,...}],"count":N,"hints":{...}}
view --json      {"result":{anonymousId,label,hme,forwardToEmail,isActive,...},"hints":{...}}
new --json       {"candidates":["a@icloud.com",...],"label":"...","hint":"..."}
new -y --json    {anonymousId,label,hme,isActive,...}
forward --json   {"forwardTo":"...","available":[...],"hint":"..."}
deactivate       {"status":"deactivated","hme":"...","id":"...","hints":{...}}
reactivate       {"status":"reactivated","hme":"...","id":"...","hint":"..."}
```

JSON output includes `hints` with suggested follow-up commands.

## Error handling

Errors include actionable fix commands:

```
Error: <ref> required — an address label, email, or ID
  Usage: ihme deactivate <ref>
  Example: ihme deactivate github.com
```

Session errors keep that shape and separate Apple's verdict on the
session from Apple having a bad day — the `Fix:` line is the whole
difference:

```
Error: iCloud rejected this session — the saved login is no longer valid
  Cause: listing HME: HTTP 401 from https://p137-maildomainws.icloud.com/v2/hme/list
  Fix: ihme auth login

Error: iCloud is temporarily unreachable — your session is probably still valid
  Cause: validating session: HTTP 502 from https://setup.icloud.com/setup/ws/1/validate
  Fix: run the same command again in a moment
```

A session that expires mid-command is re-minted once and the call
replayed, so a one-off 401 never reaches you. Only a session Apple
keeps refusing exits 2. Error text carries no account identifiers
(dsid, clientId) — safe to paste into a bug report.

## Exit codes

- `0` — success
- `1` — error (check stderr)
- `2` — authentication required — from any command, not just
  `auth status`. Run `ihme auth login`.

## Conventions

- stdout is data, stderr is status — safe to pipe
- `--yes` skips all interactive confirmation prompts
- `--verbose` / `-v` logs HTTP requests to stderr
- Session stored at `~/.config/ihme/session.json` (respects `$XDG_CONFIG_HOME`)
- Override session path with `IHME_SESSION_PATH` env var
