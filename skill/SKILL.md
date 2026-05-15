---
name: ihme
description: Manage iCloud Hide My Email addresses — list, create, edit, deactivate, export
triggers:
  - hide my email
  - HME
  - icloud email
  - private relay
  - generate email
  - email alias
  - disposable email
tools:
  - ihme
---

# ihme — iCloud Hide My Email CLI

## Setup

If `ihme` is not installed, install it:

```bash
# macOS (Apple Silicon)
curl -L https://github.com/lroolle/ihme-cli/releases/latest/download/ihme-darwin-arm64 -o /usr/local/bin/ihme && chmod +x /usr/local/bin/ihme

# macOS (Intel)
curl -L https://github.com/lroolle/ihme-cli/releases/latest/download/ihme-darwin-amd64 -o /usr/local/bin/ihme && chmod +x /usr/local/bin/ihme

# Linux
curl -L https://github.com/lroolle/ihme-cli/releases/latest/download/ihme-linux-amd64 -o /usr/local/bin/ihme && chmod +x /usr/local/bin/ihme

# Or via Go
go install github.com/lroolle/ihme-cli/cmd/ihme@latest
```

First run requires `ihme auth login` (interactive — Apple ID + 2FA).
Check with `ihme auth status --json` before other commands.

## JSON response shapes

```
list --json     → {"addresses":[{anonymousId,label,hme,isActive,createTimestamp,note,...}],"count":N,"hints":{...}}
view --json     → {"result":{anonymousId,label,hme,forwardToEmail,isActive,...},"hints":{...}}
new --json      → {"candidates":["a@icloud.com",...],"label":"...","hint":"ihme new <label> --address <addr>"}
new -y --json   → {anonymousId,label,hme,isActive,...}
forward --json  → {"forwardTo":"...","available":[...],"hint":"ihme forward set <email>"}
auth status     → {"loggedIn":true,"appleId":"...","expired":false,...}
deactivate      → {"status":"deactivated","hme":"...","id":"...","hints":{...}}
reactivate      → {"status":"reactivated","hme":"...","id":"...","hint":"..."}
```

## Commands

```bash
# Check auth first
ihme auth status --json

# List and search (325+ addresses supported)
ihme list --json --jq '.addresses[0:5]'
ihme list --search netflix --json
ihme list --active --tag dev --json
ihme list --sort label --json

# Create (two-step: generate candidates, then reserve)
ihme new github.com --json                              # step 1: get candidates
ihme new github.com --address abc@icloud.com --json     # step 2: reserve one
ihme new github.com --yes --json                        # one-shot: take first

# View and edit
ihme view github.com --json --jq '.result.hme'
ihme edit github.com --label GitHub --tag dev,work

# Lifecycle
ihme deactivate github.com --json
ihme reactivate github.com --json
ihme delete github.com --yes --json

# Export
ihme export --format json
ihme export --search github --active -o filtered.csv

# Forward-to
ihme forward --json
ihme forward set user@icloud.com
```

## <ref> resolution

All commands accepting `<ref>` resolve in order: anonymousId > email > label (exact) > label (fuzzy).

## Error handling

Errors include the fix command:
```
Error: <ref> required — an address label, email, or ID
  Usage: ihme deactivate <ref>
  Example: ihme deactivate github.com
```

## Exit codes

- 0: success
- 1: error
- 2: not authenticated (run `ihme auth login`)
