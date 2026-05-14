---
name: ihme
description: Manage iCloud Hide My Email addresses from the command line
triggers:
  - hide my email
  - HME
  - icloud email
  - private relay
  - generate email address
  - manage email aliases
  - disposable email
  - email alias
tools:
  - ihme
---

# ihme — iCloud Hide My Email CLI

Manage iCloud Hide My Email addresses. Requires iCloud+ subscription and prior authentication via `ihme auth login`.

## Commands

```bash
# Auth
ihme auth login              # Sign in with Apple ID (SRP + 2FA)
ihme auth status             # Check session
ihme auth logout             # Clear session

# List and filter
ihme list                    # All addresses (table)
ihme list --json             # JSON output
ihme list --active           # Active only
ihme list --tag dev          # Filter by tag
ihme list --json --jq '.[].hme'  # Extract just addresses

# Create
ihme new github.com                          # Generate + reserve
ihme new github.com --note "main" --tag dev  # With metadata

# View and edit
ihme view github.com         # Detail view (resolves by label, email, or ID)
ihme edit github.com --label "GitHub" --tag dev,work
ihme copy github.com         # Copy address to clipboard

# Lifecycle
ihme deactivate github.com   # Stop receiving mail
ihme reactivate github.com   # Resume
ihme delete github.com -y    # Permanent removal

# Export
ihme export                  # CSV to stdout
ihme export --format json -o addresses.json
ihme export --active --tag dev

# Forward-to
ihme forward                 # Show current
ihme forward set new@icloud.com
```

## Output

Every command supports `--json` and `--jq` for machine-readable output:

```bash
ihme list --json --jq '.[] | select(.isActive == false) | .hme'
ihme view github.com --json
ihme new example.com --json
```

## Reference resolution

`<ref>` arguments accept: anonymousId, full email address, or label (exact then fuzzy match).

## Exit codes

- 0: success
- 1: error
- 2: not authenticated
