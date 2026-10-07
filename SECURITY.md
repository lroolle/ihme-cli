# Security

## Local storage

`ihme` stores session tokens, trust tokens, cookies, Apple account identifiers, and webservice URLs in its session file. These are credentials: anyone who obtains them may be able to act on the account.

- macOS / Linux: `~/.config/ihme/session.json` (respects `XDG_CONFIG_HOME`)
- Windows: `%AppData%\ihme\session.json`
- Override: `IHME_SESSION_PATH`

Session writes are atomic and request owner-only file permissions (`0600` on Unix). Protect the file with your operating system's account and filesystem permissions. Atomic writes do not serialize concurrent CLI processes.

The Apple ID password and 2FA codes are not saved. Authentication uses SRP-6a; the password itself is not transmitted. Trust tokens can allow later sign-ins without a new 2FA challenge; Apple controls their lifetime.

## Shared inboxes

- `shares.json`, beside the session file (`IHME_SHARES_PATH`): each shared address, its label, and the SHA-256 of its key. Never the key. Atomic writes, `0600`.
- `ihme serve` reads mail with an IMAP app-specific password: `IHME_IMAP_PASSWORD`, or the one em saved in `~/.config/em/accounts.json` (em writes it `0600`; ihme only reads it). That password reads the whole mailbox: keep it out of shell history and logs. The server never touches the iCloud session.
- A key opens one address. A message shows only if its envelope says it was sent To or Cc that address, inside `--days`. Message links are sealed with a per-process key and bound to the visitor's address.
- The browser holds the key in an HttpOnly, SameSite=Lax cookie for 7 days. It is Secure when the request came over TLS or with `X-Forwarded-Proto: https`, and always when `IHME_SERVE_URL` is an https URL.
- A share link carries the key in its path (`/k/<key>`): it lands in browser history and in any access log that records paths.
- `ihme share revoke` takes effect on the next request. Two share commands run at the same instant can lose one's write (no file lock yet); check `ihme share list` after scripting several.

## Agent mode

Model keys may be stored in the local ihme configuration or environment. Agent memory contains aliases, labels, notes, preferences, and task history in plain Markdown files. Treat both as private.

Agent tasks and tool results go to your selected model provider or coding agent. Direct CLI commands do not use an LLM. `ihme auth logout` clears the saved Apple session, not model configuration or agent memory.

## Report a vulnerability

Use GitHub's [private vulnerability reporting](https://github.com/lroolle/ihme-cli/security/advisories/new). Include the affected version, impact, and steps to reproduce; omit real tokens and account data. Do not open a public issue with an exploit or credentials.

Security fixes target the latest release. Older versions are not separately maintained; there is no guaranteed response time.

## Service boundary

This is an unofficial client of Apple's undocumented iCloud web API. Apple can change or block access. This project cannot guarantee account availability, rate limits, or continued compatibility.
