# Command guide

Create, find, edit, and export iCloud Hide My Email addresses from the terminal.

[Install and sign in](../README.md#install) before running these commands.

## Commands


```
AUTH
  ihme auth login                Sign in with Apple ID (SRP + 2FA)
  ihme auth status [--json]      Session state
  ihme auth logout               Clear session

LIST & SEARCH
  ihme list                      All addresses (table)
  ihme list --search <query>     Search label, address, or note
  ihme list --active             Only active
  ihme list --tag <tag>          Filter by tag
  ihme list --sort label         Sort by label or date

CREATE
  ihme new <label>               Interactive: pick from ~3 candidates
  ihme new <label> --yes         Script: take first
  ihme new <label> --json        Agent: get candidates without reserving
  ihme new <label> --agent       Embedded agent runs the full procedure

AGENT (built-in, BYOK)
  ihme agent                     Interactive session (inline TUI)
  ihme agent "<task>"            One-shot: "deactivate my old figma alias"
  ihme agent -p "<task>"         Same, via --prompt/-p (also: ihme -p "<task>")
  ihme agent --via claude "<task>"  Harness claude-code/codex/opencode as the
                                 provider (subscription auth, no API key)
  ihme agent --grant auto        Skip consent prompts for this run
  ihme memory                    Inspect the agent's memory graph
  ihme memory search <query>     Search journals and pages
  ihme memory graph              Show topic pages and backlinks

MANAGE (omit <ref> at a terminal to pick, newest first)
  ihme view <ref>                View details
  ihme edit <ref>                Edit label, note, tags
  ihme copy <ref>                Copy address to clipboard
  ihme deactivate <ref>          Stop receiving mail
  ihme reactivate <ref>          Resume receiving mail
  ihme delete <ref> [--yes]      Permanent deletion (confirms first)

SHARE AN INBOX
  ihme share <ref>               Access key + link for one address
  ihme share list                Addresses with a live key
  ihme share revoke <ref>        Stop a key working (--all: every key)
  ihme serve                     Plain web inbox for shared addresses

EXPORT
  ihme export                    CSV to stdout
  ihme export --format json      JSON to stdout
  ihme export -o file.csv        To file
  ihme export --search dev       Filtered export

FORWARD
  ihme forward [--json]          Show forward-to address
  ihme forward set <email>       Change it
```

`<ref>` resolves by: anonymousId (full or 6+ char prefix) > email > label (exact) > label (fuzzy).

Leave `<ref>` out at a terminal and ihme asks which address, newest first; Enter takes the one created most recently. `ihme new netflix`, then `ihme copy` and Enter, copies the new address. Pipes, scripts, and `--json` never get a prompt: they get the usage error. A harness that runs ihme inside a pseudo-terminal can set `IHME_NO_PROMPT` (any value) for the same behavior.

## JSON and jq

Use `--json` for structured address data; response shapes are documented in `ihme <cmd> --help`. `--jq` calls an installed `jq` executable. Combine it with `--json`. Commands such as `copy`, `version`, and interactive login print human output even when given the global `--json` flag.

```bash
ihme list --json --jq '.addresses[0:5]'
ihme list --search github --json --jq '.addresses[].hme'
ihme list --json --jq '.count'
ihme view github.com --json --jq '.result.hme'
ihme new mysite.com --json | jq '.candidates'
```


## Create without surprises

```bash
ihme new github.com --json                     # generate only
ihme new github.com --address <candidate> --json # reserve your choice
ihme new github.com --yes --json                # reserve the first candidate
```

Replace `<candidate>` with an address returned by the first command. A terminal session with `ihme new github.com` asks you to pick; without a terminal, that same command reserves the first candidate. Use `--json` alone when you only want candidates.

## Tags and notes

```bash
ihme new example.com --tag shopping --note "main account"
ihme list --tag shopping
ihme edit example.com --label Shopping --tag shopping,personal
```

Tags use the `#shopping | main account` note convention shared with the [Hide My Email browser extension](https://github.com/dedoussis/icloud-hide-my-email-browser-extension).

## Lifecycle

```bash
ihme deactivate example.com     # stop forwarding
ihme reactivate example.com     # resume forwarding
ihme delete example.com --yes   # permanently delete an inactive address
```

Deactivate an address before deleting it. Deactivation and reactivation take effect immediately; deletion asks for confirmation unless `--yes` is set. A picked address is always confirmed, even with `--yes`; without a terminal, `--yes` is required.

## Share an inbox

One address's mail on a plain web page. Hand someone its key: they read that address and nothing else. Netflix codes for family; your own codes on a phone.

```bash
ihme share netflix          # key + link, printed once
ihme serve                  # http://127.0.0.1:8025
ihme share list             # what is shared
ihme share revoke netflix   # locked out on the next request
```

IMAP login, first match wins:

- `--account <email>`: that account in em's `~/.config/em/accounts.json` (read only)
- `IHME_IMAP_USER` + `IHME_IMAP_PASSWORD`: your @icloud.com address and an [app-specific password](https://support.apple.com/102654). `IHME_IMAP_SERVER` (host:port, TLS) if your addresses forward to a non-iCloud mailbox
- em's accounts file, when it holds exactly one account

What the visitor gets:

- INBOX and Junk mail sent To or Cc their address in the last 30 days (`--days`). Older mail does not open even by URL. Bcc-only mail does not show.
- Plain text. No scripts, images, or fonts; mail HTML becomes text on the server. Other recipients are never shown.
- Message links sealed to their address. They die on restart and cannot be guessed, counted, or opened with another key.

What the server can do: read mail. Not create, delete, or redirect addresses; viewing does not mark mail read. ihme stores key hashes only; sharing an address again rotates its key.

Going public: there is no admin page, shares are managed with `ihme share` on the server's machine. Put HTTPS in front and set `IHME_SERVE_URL=https://...` for both commands: `ihme share` prints working links, and `ihme serve` always marks the key cookie Secure. Have the proxy send `X-Forwarded-Proto: https` too (Caddy does; nginx: `proxy_set_header X-Forwarded-Proto $scheme;`). `/k/<key>` paths carry the key: keep them out of access logs.

## Authentication and errors

`ihme auth login` signs in interactively with Apple ID and two-factor authentication. `ihme auth status --json` checks the saved session against iCloud; add `--local` to inspect only the local file.

Exit codes: `0` success, `1` other error, `2` authentication required. A rejected session is refreshed once during an HME call; if Apple still refuses it, run `ihme auth login`. A temporary network or server failure asks you to retry instead.

Session location and stored data are documented in [Security](../SECURITY.md). Apple controls rate limits and candidate availability; repeat generation can return the same pool. There is no guaranteed creation quota.
