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

Prefer the local checkout when available, so agents use the newest CLI changes:

```bash
cd worktree/ihme-cli
make install
```

`make install` builds `ihme` and copies it to `$GOPATH/bin/ihme`, or to
`~/go/bin/ihme` when `$GOPATH` is unset. Make sure that directory is on `PATH`.

If the checkout is unavailable, install a release:

```bash
# macOS (Apple Silicon)
curl -sL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_arm64.tar.gz | tar xz && sudo mv ihme /usr/local/bin/

# macOS (Intel)
curl -sL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_x86_64.tar.gz | tar xz && sudo mv ihme /usr/local/bin/

# Linux
curl -sL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_linux_x86_64.tar.gz | tar xz && sudo mv ihme /usr/local/bin/

# Or via Go
go install github.com/lroolle/ihme-cli/cmd/ihme@latest
```

First run requires `ihme auth login` (interactive — Apple ID + 2FA).

Session lookup uses `IHME_SESSION_PATH` when set; otherwise it reads
`$XDG_CONFIG_HOME/ihme/session.json`, falling back to `~/.config/ihme/session.json`.

`ihme auth status --json` first reads the local session file, then checks whether
that session can currently access iCloud. It includes Apple's `/validate` payload
as `rawResponse`. Use `ihme auth status --local --json` only when you want the
local file/timestamp check without a network request.
`--verbose` only logs method, URL, status, and size.
Current `/validate` success responses are account-info payloads with `dsInfo`
and `webservices`; they may omit a `success` boolean.

## JSON response shapes

```
list --json     → {"addresses":[{anonymousId,label,hme,isActive,createTimestamp,note,...}],"count":N,"hints":{...}}
view --json     → {"result":{anonymousId,label,hme,forwardToEmail,isActive,...},"hints":{...}}
new --json      → {"candidates":["a@icloud.com",...],"label":"...","hint":"ihme new <label> --address <addr>"}
new -y --json   → {anonymousId,label,hme,isActive,...}
forward --json  → {"forwardTo":"...","available":[...],"hint":"ihme forward set <email>"}
auth status     → {"loggedIn":true,"appleId":"...","expired":false,"canAccessICloud":true,"rawResponse":{...},...}
deactivate      → {"status":"deactivated","hme":"...","id":"...","hints":{...}}
reactivate      → {"status":"reactivated","hme":"...","id":"...","hint":"..."}
```

## Commands

```bash
# Auth check: local file + current iCloud access
ihme auth status --json

# Local auth file/timestamp only
ihme auth status --local --json

# List and search (hundreds of addresses supported)
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

All commands accepting `<ref>` resolve in order: anonymousId (prefix >= 6 chars) > email > label (exact) > label (fuzzy).

## Choosing an address

You'll see this address for years — in password managers, email threads, account
settings. It's a mask, but it's still yours. The core test:

**Does it make a picture, and is the picture one you'd keep?**

Two checks, in order:

1. **Can you see it?** Concrete nouns with physicality beat abstractions.
   `hilltop_desert` is a landscape. `pollen_pipe` is an object. `63.fryer.immune`
   is a serial number. Specific things are memorable; categories are forgettable.

2. **Would you keep it?** No deficit words (debts, gristle, paupers), no clinical
   tone (immune, generic, baseline), nothing that carries weight. An address is a
   micro-identity — it shouldn't feel assigned.

Bonus (elevates good to great, not a gate):

- **Contextual resonance**: does the address echo something about the service —
  a brand name, a logo shape, a developer handle? `oranges.lobby` for a service
  whose developer is `@oran_ge` and whose logo is an O isn't luck — it's fit.
  Most addresses won't have this. When it's there, it's decisive.

Secondary signals (tiebreakers, not filters):

- **Euphony**: read it aloud. Pleasant vowel/consonant rhythm and natural stress
  help recognition in a list of 300+.
- **No leading digits**: `65.ampere` reads like a form field. Letters first.
- **Separator style**: a distant tiebreaker. Never override a better image for a
  preferred separator. `hilltop-desert` beats `relay_strop` regardless of format.

The best HME addresses feel like they could be a place on a map, a cocktail name,
or an album title — evocative without trying.

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

## Operational guide (for agents)

### Creating an address

1. **Derive the label and search key.** For a URL, use the registrable domain
   without public suffix as the canonical label/search key: `https://atypica.ai/...`
   becomes `atypica`; `https://linear.app/...` becomes `linear`. Drop paths,
   query strings, callback URLs, referral parameters, dates, and campaign text.
   Keep the full URL only as context for the user or note.

2. **Check auth.** Confirm the stored session can access iCloud before generating
   candidates:
   ```bash
   ihme auth status --json
   ```
   If it exits 2 or returns `canAccessICloud:false`, the user must run
   `ihme auth login` interactively.

3. **Check first.** Search for existing addresses before creating:
   ```bash
   ihme list --search <search-key> --json
   ```
   `--search` matches label, address, and note by substring, so search the
   canonical key, then inspect returned labels for the intended service.
   Interpret matches by label, not by search key:
   - An active address with the SAME canonical label is a duplicate: ask
     before creating another (embedded interactive runs: `ask_user`). When
     you cannot ask, an explicit `new <label>` request has already decided
     creation — proceed, and flag the existing duplicate prominently in the
     summary.
   - Addresses for the same service under DIFFERENT labels (older accounts,
     dated labels, per-team variants) are context, never a blocker: mention
     them in the note or summary and continue.

4. **Generate and evaluate.** Get candidates and apply the taste test above:
   ```bash
   ihme new <label> --json
   ```
   Evaluate each candidate individually — don't let bad neighbors taint a good
   one. A pool with two duds and one strong image is not a "weak pool."
   Reserve the best immediately — don't ask unless no single candidate passes
   taste ("does it make a picture you'd keep?"). When NO candidate passes
   after rotation: interactively (embedded: `ask_user`), offer your top two
   with a one-line image each and let the user pick; non-interactively,
   reserve the least-bad and say plainly it was a compromise. Embedded runs
   must articulate the verdict: `reserve_address` requires `rationale` (the
   winner's image and why it fits) plus one `rejected` entry per candidate
   you passed on, each naming its failure — the user judges your pick
   against these on the consent card.

5. **Reserve with a useful note.** `ihme new` supports `--note`; Apple stores it
   in the address metadata, and `ihme list --search` searches it. Keep notes
   compact and durable: why the address exists, the full signup/origin URL when
   useful, account/workspace context, referrer/invite code, or owner/team. Do not
   put passwords, recovery codes, API keys, cookies, or other secrets in notes.
   ```bash
   ihme new <label> --address <candidate> --note "signup: https://example.com/auth/signup?via=team; workspace: acme" --json
   ```

6. **Refresh the pool if it is weak.** Apple's generate returns a
   FIXED pending pool that repeats — calling generate again returns
   the SAME candidates until a slot is consumed. So if no candidate
   passes taste and the pool stops changing, do not keep generating.
   Consume a slot to force a fresh pool: reserve a throwaway and
   immediately delete it, then generate again.
   - Embedded agent: `refresh_candidates` does the whole maneuver in
     one call (reserve + delete + regenerate), capped at 2 per task.
   - Shell: reserve any candidate, delete it, then generate again.
     ```bash
     ihme new <label> --address <throwaway> --json   # consume a slot
     ihme delete <throwaway-id> --yes --json          # clean it up
     ihme new <label> --json                          # fresh pool
     ```
   - If the refreshed pool still has no keeper, stop churning: take
     the least-bad and say plainly it was a compromise. Never ask the
     user to restart the session — you get a fresh budget next request.

7. **Show the result.** State what was reserved and one line on why it was
   picked. If a compromise was made (weak pool, no good images), say so.

### Labels

Use the service or team name as a bare noun. Dates age; names don't.
- Good: `github`, `linear`, `colaos`, `atypica`
- Avoid: `240501_chatgpt openai`, `0315 claude felix 2`, full signup URLs,
  referral/callback parameters

### Tags

Apply from a small controlled set when the user specifies context.
Common tags: `#work`, `#dev`, `#personal`, `#throwaway`, `#team-<name>`.

### Hygiene

`ihme list --sort date:asc` to surface old addresses for audit.
Suggest pruning dead services quarterly.

### Memory

You keep a memory across runs — a plain markdown graph (journals for
what you did, pages per topic, a flashcards page loaded into every
run). Use it for continuity:

- **Recall before creating.** Search memory for the service first;
  you may have reserved for it before, and the past note carries the
  account context. Shell: `ihme memory search <service>`. Embedded:
  `recall_memory`.
- **Reservations journal themselves.** Every reserve is written to
  memory automatically, linked to its service page. Never hand-record
  a reservation.
- **Remember durable learnings, sparingly.** When you learn a lasting
  preference or a fact about a service worth carrying forward, save
  one note. Pin it to the `flashcards` topic to have it loaded into
  every future run; use any other topic for on-demand recall. Never
  store secrets. Shell: `ihme memory card <note>`. Embedded:
  `remember`.

## Execution adapters

The procedure and taste rules above are shared by two executors; only
the operation mapping differs.

**External agent** (Claude Code etc.): run the shell commands as
written above.

**Embedded agent** (`ihme new <label> --agent` for scoped creation,
`ihme agent` for the general interactive assistant): the same file is
embedded in the binary and invoked with the user's task. There is no
shell — operations map to in-process tools:

| Shell command | Embedded tool |
|---|---|
| `ihme auth status --json` | `auth_status` |
| `ihme list --search <key> --json` | `search_addresses` |
| `ihme new <label> --json` (candidates) | `generate_candidates` |
| reserve + delete a throwaway, then generate | `refresh_candidates` |
| `ihme new <label> --address <a> --note <n> --json` | `reserve_address` |
| `ihme deactivate <ref> --json` | `deactivate_address` |
| `ihme edit <ref> ...` | `edit_note` |
| `ihme memory search <query>` | `recall_memory` |
| `ihme memory card <note>` (or editing a page) | `remember` |

Embedded runs enforce the rotation cap (3 generation rounds) and
call budgets in code, and gate mutating actions outside the run's
granted scope behind user consent (`--grant ask`, the default) or
allow them unattended (`--grant auto`). Interactive embedded runs
also expose `ask_user` — one short question, answered on the
terminal, max 3 per run. Non-interactive runs must decide within
the task scope and record assumptions instead of stalling.
