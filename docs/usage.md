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
  ihme new <label> --share       New address, with the key + link for its mail
  ihme share <ref> --copy        Key + link; copy the link to clipboard
  ihme share config --url <url> Save the default remote inbox URL
  ihme share secret              The secret your ihme-inbox server needs

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

After a reservation at a terminal, the address is copied automatically using the platform clipboard tool. `--share` copies its inbox link instead. `--no-copy` disables this; `--copy` also works with JSON or piped output. Automatic copying is disabled for JSON, `--jq`, piped stdout, and `IHME_NO_PROMPT`. Clipboard status goes to stderr; an unavailable clipboard never fails a reservation.

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

One address's mail on a plain web page. Send someone the link: they read that address and nothing else. Netflix codes for family; your own codes on a phone.

The page is served by [ihme-inbox](https://github.com/lroolle/ihme-inbox), a separate small program. ihme makes the addresses and their keys; ihme-inbox shows the mail. Through v0.7.0 this was `ihme serve`.

```bash
ihme share secret           # once: creates the secret, prints the line the server needs
ihme new netflix --share    # a new address, its key, its link
ihme share netflix          # key and link for an address you already have
```

Save your remote inbox once, then copy links in one command:

```bash
ihme share config --url https://mail.example.com
ihme share netflix --copy
```

The saved URL applies to both `share` and `new --share`. Precedence: `share --url`, `IHME_INBOX_URL`, legacy `IHME_SERVE_URL`, saved URL, then `http://127.0.0.1:8025`. `ihme share config` shows the effective URL. The URL is stored in `~/.config/ihme-inbox/url` (respecting `IHME_INBOX_DIR` and `XDG_CONFIG_HOME`); saving it never changes the secret. Remote URLs require HTTPS.

`--copy` puts only the link on the clipboard. If the clipboard is unavailable, the link is still printed. Clipboard status goes to stderr so `--json` and `--jq` remain usable. Anyone holding a link can read that address's mail and, on an inbox server with deletion support, hide or restore messages in that shared inbox; the original mailbox stays unchanged.

- A key is computed from the address, the time, and the secret. Nothing is stored and the server is not contacted, so a new address works there at once.
- The secret is `IHME_INBOX_SECRET`, else the file `~/.config/ihme-inbox/secret`, which ihme-inbox reads too: on one machine the two agree with no setup. Only `ihme share secret` creates it; `share` and `new --share` stop with that hint when it is missing.
- `ihme share` again prints another key. Earlier ones keep working.
- Revoke on the server's machine: `ihme-inbox revoke <address>`. It stops every key made so far for that address, on the next request. For a new key, make it there (`ihme-inbox key <address>`) or wait five seconds before `ihme share`: a key carries this machine's time, and a revoke also covers keys dated up to 5 seconds ahead of the server's clock. Keep both machines on network time. A link that says "not valid yet" means this machine's clock runs ahead.
- `ihme new --share` checks for the secret before it reserves anything. With `--json` it needs `--yes` or `--address`; the reservation gains a `share` field: `{"key":"...","link":"...","mintedAt":"..."}`.

Running the server, what a visitor sees, and what the server can read: the [ihme-inbox README](https://github.com/lroolle/ihme-inbox#readme).

Coming from v0.7.0: keys from that version open nothing now; share again. `IHME_SERVE_URL` is still read where `IHME_INBOX_URL` is not set. `shares.json` beside the session file is unused and can be deleted.

## Authentication and errors

`ihme auth login` signs in interactively with Apple ID and two-factor authentication. `ihme auth status --json` checks the saved session against iCloud; add `--local` to inspect only the local file.

Exit codes: `0` success, `1` other error, `2` authentication required. A rejected session is refreshed once during an HME call; if Apple still refuses it, run `ihme auth login`. A temporary network or server failure asks you to retry instead.

Session location and stored data are documented in [Security](../SECURITY.md). Apple controls rate limits and candidate availability; repeat generation can return the same pool. There is no guaranteed creation quota.
