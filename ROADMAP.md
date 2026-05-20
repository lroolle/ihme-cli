# Roadmap

Current: **v0.1.3** · 4.1k LOC · 5 deps · [MIT](LICENSE)

## v0.2 — Fix what the review found

| Item | Why |
|------|-----|
| Silent 2FA push failure — log error, fall back to code prompt | Users wait forever when push silently fails |
| Note clearing — send empty field, don't omit it | `ihme edit foo --note ""` is a no-op today |
| `jq` presence check with install hint | "exec: jq: not found" is not actionable |
| CI lint — bump golangci-lint for go1.26 | CI red since v0.1.0, unrelated to code quality |
| `forward set` basic email validation | Server rejects garbage but with a worse message |
| Generate dupe warning when fewer candidates returned | Silent short-count surprises scripts |

## v0.3 — Session resilience

| Item | Why |
|------|-----|
| Auto session resume on 401/expired during any command | Currently fails; user must re-run `auth login` |
| Retry with backoff on 5xx / transient errors (1 retry, 3s) | Apple's API is flaky under load |
| Session refresh without triggering Apple sign-in email | `accountLogin` sends a "new sign-in" notification every time |
| Lock file for concurrent access | Parallel agent invocations corrupt session.json |

## v0.4 — Agent-native features

| Item | Why |
|------|-----|
| `ihme audit` — surface old addresses, inactive services, missing tags | 326 addresses, 1 deactivated. Hygiene at scale |
| `ihme bulk deactivate --tag throwaway --older-than 6m` | Batch lifecycle for agent-driven cleanup |
| `ihme new --taste` — score candidates by euphony/affect/rhythm | Codify address selection instead of random pick |
| Structured stderr (JSON errors when `--json` is set) | Agents can't parse human error messages reliably |
| Shell completions with dynamic ref completion | Tab-complete labels and IDs for humans |

## v0.5 — Distribution

| Item | Why |
|------|-----|
| Homebrew tap (`brew install lroolle/tap/ihme`) | Primary install path for macOS |
| AUR package | Linux distribution |
| README demo GIF (asciinema) | The README sells; the code ships |
| Issue templates (bug, feature) | Signal maturity, structure contributions |

## v1.0 — Production grade

| Item | Why |
|------|-----|
| `api/` test coverage 16.9% -> 60%+ via recorded HTTP fixtures | Auth flow is the hardest code and least tested |
| API change detection — weekly CI probe against Apple | Undocumented API = silent breakage |
| Keyring integration (macOS Keychain, GNOME Keyring) | Optional alternative to plaintext session JSON |
| Stable JSON output contract (versioned, no breaking changes) | Agents and scripts depend on response shapes |
| Man pages from Cobra | Unix convention |

## Strategic position

Only open-source CLI for iCloud Hide My Email that works end-to-end. pyicloud doesn't do HME. rclone does storage. Go-iClient is a library.

The moat is operational knowledge of Apple's undocumented auth protocol — SRP-6a with custom modifications, mandatory 2FA, trust tokens, CN routing. This breaks without warning and requires re-reverse-engineering.

Optimize for:
1. **Resilience** — sessions that survive, retries that work, change detection
2. **Agent ergonomics** — structured output, taste scoring, bulk ops
3. **Distribution** — people can't use what they can't install
