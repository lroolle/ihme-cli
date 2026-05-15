# ihme-cli: iCloud Hide My Email CLI

A standalone CLI for iCloud's Hide My Email service. Designed for humans and AI agents.

`gh` for GitHub. `atl` for Atlassian. `ihme` for iCloud Hide My Email.


## Why

The browser extension works. But:

- You can't script it. No cron cleanup, no bulk import, no CI integration.
- You can't pipe it. No `ihme list --json | jq '.[] | select(.isActive == false)'`.
- AI agents can't use it. Claude Code can run `gh pr list` but can't touch your HME addresses.
- Auth is tied to a browser session. No headless operation.

A CLI fixes all of that. One binary, no browser, composable with everything.


## Commands

```
ihme auth login              # Apple ID SRP auth + 2FA, store session
ihme auth status             # Show current auth state
ihme auth logout             # Clear stored session

ihme list                    # All addresses, table format
ihme list --json             # Structured output for agents
ihme list --active           # Only active
ihme list --inactive         # Only inactive
ihme list --tag shopping     # Filter by tag
ihme list --jq '.[].hme'    # Built-in jq filtering

ihme new <label>             # Generate + reserve in one shot
ihme new github.com --note "main account" --tag dev

ihme view <ref>              # View one address (by ID, label, or email)
ihme edit <ref>              # Edit label, note, tags
ihme copy <ref>              # Copy address to clipboard

ihme deactivate <ref>        # Stop receiving mail
ihme reactivate <ref>        # Resume receiving mail
ihme delete <ref>            # Permanent removal (must be inactive)

ihme export                  # CSV to stdout
ihme export --json           # JSON to stdout
ihme export --tag dev -o dev-addresses.csv

ihme forward                 # Show current forward-to address
ihme forward set <email>     # Change forward-to
```

`<ref>` is a universal resolver: accepts anonymousId, label (fuzzy match), or email address.


## Auth

Apple's auth flow:

```
Apple ID + password
    |
    v
SRP (Secure Remote Password) handshake
    |
    v
2FA code (SMS/device push)
    |
    v
Trust token (long-lived, stored locally)
    |
    v
Session cookies -> API access
```

### Implementation

The `ihme auth login` flow:

1. Prompt for Apple ID email + password (or read from env/keychain)
2. SRP handshake with `setup.icloud.com/setup/ws/1/login`
3. Prompt for 2FA code (interactive) or accept `--code <code>` flag
4. Store trust token + session in `~/.config/ihme/session.json` (encrypted)
5. Subsequent commands use stored session; auto-refresh on expiry

Reference implementations that have solved this:
- [pyicloud](https://github.com/picklepete/pyicloud) — Python, mature, handles SRP + 2FA
- [icloud-api](https://github.com/nicories/icloud-api) — Rust, clean SRP implementation
- Our own `iCloudClient.ts` — the extension's API layer (thin fetch wrapper over validated session)

### Session storage

```
~/.config/ihme/
  config.yaml       # preferences (default format, editor, etc.)
  session.json      # encrypted session tokens (trust token, cookies, webservices)
```

Config supports env var override: `IHME_APPLE_ID`, `IHME_SESSION_PATH`.


## Architecture

```
ihme (binary)
  |
  +-- cmd/           # Cobra commands
  |     auth.go      # login, status, logout
  |     list.go      # list with filters
  |     new.go       # generate + reserve
  |     view.go      # view single address
  |     edit.go      # edit metadata
  |     lifecycle.go  # deactivate, reactivate, delete
  |     export.go    # csv/json export
  |     forward.go   # forward-to management
  |
  +-- api/           # iCloud API client
  |     client.go    # HTTP client with auth headers
  |     srp.go       # SRP auth implementation
  |     hme.go       # Hide My Email endpoints
  |     session.go   # Session persistence + refresh
  |
  +-- pkg/
  |     output/      # Table, JSON, CSV formatters
  |     resolver/    # Universal ref -> anonymousId resolution
  |     tags/        # Tag parsing (#tag convention from extension)
  |
  +-- internal/
        config/      # Viper config loading
        keychain/    # OS keychain integration (optional)
```

### Language: Go

- Single binary, no runtime
- Cobra + Viper (same as gh, atlas-cli)
- Cross-platform (macOS, Linux, Windows)
- `go install github.com/lroolle/ihme-cli@latest`


## Output design

Every command supports `--json` and `--jq`:

```bash
# Human-readable (default)
$ ihme list --active
ID          LABEL           ADDRESS                              CREATED     STATUS
a1b2c3d4    github.com      abc123@privaterelay.appleid.com      Jan 15      active
e5f6g7h8    amazon.com      def456@privaterelay.appleid.com      Jan 10      active

# Machine-readable
$ ihme list --active --json
[
  {
    "anonymousId": "a1b2c3d4",
    "label": "github.com",
    "hme": "abc123@privaterelay.appleid.com",
    "isActive": true,
    "createTimestamp": 1705276800000,
    "tags": ["dev"],
    "note": "main account"
  }
]

# Inline filtering
$ ihme list --json --jq '.[].hme'
abc123@privaterelay.appleid.com
def456@privaterelay.appleid.com
```


## Claude Code skill

Ships as a skill that auto-triggers on HME-related requests:

```
~/.claude/skills/ihme-cli/
  SKILL.md          # Skill metadata + trigger patterns
```

Triggers on: "hide my email", "HME", "icloud email", "private relay",
"generate email address", "manage email aliases".

Example agent interaction:
```
User: "Create a new hide my email for netflix"
Claude Code: $ ihme new netflix.com
  Reserved: xyz789@privaterelay.appleid.com (label: netflix.com)
Claude Code: "Created xyz789@privaterelay.appleid.com for netflix.com."
```


## Scope

### v0.1 — Core

- [ ] SRP auth + 2FA + session persistence
- [ ] `list`, `new`, `view`, `edit`
- [ ] `deactivate`, `reactivate`, `delete`
- [ ] `export` (CSV, JSON)
- [ ] `--json` + `--jq` on all commands
- [ ] Claude Code skill

### v0.2 — Quality of life

- [ ] `copy` (clipboard integration)
- [ ] `forward` management
- [ ] Fuzzy label matching in `<ref>` resolver
- [ ] Shell completions (bash, zsh, fish)
- [ ] `ihme auth login --headless` (env var auth for CI)

### v0.3 — Agent-native

- [ ] `ihme watch` — daemon mode, webhook on new addresses
- [ ] `ihme cleanup` — interactive or scripted bulk cleanup
- [ ] `ihme sync` — bidirectional sync with local file (YAML/JSON)
- [ ] Homebrew tap


## Prior art

| Tool | Platform | Auth | Pattern we borrow |
|------|----------|------|-------------------|
| `gh` | GitHub | OAuth + token | Command hierarchy, `--json`/`--jq`, extensions |
| `atl` | Atlassian | Bearer token | Skill-native CLI, agent-first output, Go + Cobra |
| `pyicloud` | iCloud | SRP + 2FA | Auth flow implementation |
| Our extension | iCloud HME | Session cookies | API surface (`PremiumMailSettings` methods) |


## Non-goals

- Not a general iCloud CLI (no Drive, Photos, Contacts)
- Not an MCP server (CLI is the tool-use protocol)
- Not interactive TUI (breaks agent integration)
- Not a replacement for the browser extension (different use case)
