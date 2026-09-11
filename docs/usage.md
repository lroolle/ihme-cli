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

MANAGE
  ihme view <ref>                View details
  ihme edit <ref>                Edit label, note, tags
  ihme copy <ref>                Copy address to clipboard
  ihme deactivate <ref>          Stop receiving mail
  ihme reactivate <ref>          Resume receiving mail
  ihme delete <ref> [--yes]      Permanent deletion (confirms first)

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

Deactivate an address before deleting it. Deactivation and reactivation take effect immediately; deletion asks for confirmation unless `--yes` is set.

## Authentication and errors

`ihme auth login` signs in interactively with Apple ID and two-factor authentication. `ihme auth status --json` checks the saved session against iCloud; add `--local` to inspect only the local file.

Exit codes: `0` success, `1` other error, `2` authentication required. A rejected session is refreshed once during an HME call; if Apple still refuses it, run `ihme auth login`. A temporary network or server failure asks you to retry instead.

Session location and stored data are documented in [Security](../SECURITY.md). Apple controls rate limits and candidate availability; repeat generation can return the same pool. There is no guaranteed creation quota.
