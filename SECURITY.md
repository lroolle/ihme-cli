# Security

## Local storage

`ihme` stores session tokens, trust tokens, cookies, Apple account identifiers, and webservice URLs in its session file. These are credentials: anyone who obtains them may be able to act on the account.

- macOS / Linux: `~/.config/ihme/session.json` (respects `XDG_CONFIG_HOME`)
- Windows: `%AppData%\ihme\session.json`
- Override: `IHME_SESSION_PATH`

Session writes are atomic and request owner-only file permissions (`0600` on Unix). Protect the file with your operating system's account and filesystem permissions. Atomic writes do not serialize concurrent CLI processes.

The Apple ID password and 2FA codes are not saved. Authentication uses SRP-6a; the password itself is not transmitted. Trust tokens can allow later sign-ins without a new 2FA challenge; Apple controls their lifetime.

## Agent mode

Model keys may be stored in the local ihme configuration or environment. Agent memory contains aliases, labels, notes, preferences, and task history in plain Markdown files. Treat both as private.

Agent tasks and tool results go to your selected model provider or coding agent. Direct CLI commands do not use an LLM. `ihme auth logout` clears the saved Apple session, not model configuration or agent memory.

## Report a vulnerability

Use GitHub's [private vulnerability reporting](https://github.com/lroolle/ihme-cli/security/advisories/new). Include the affected version, impact, and steps to reproduce; omit real tokens and account data. Do not open a public issue with an exploit or credentials.

Security fixes target the latest release. Older versions are not separately maintained; there is no guaranteed response time.

## Service boundary

This is an unofficial client of Apple's undocumented iCloud web API. Apple can change or block access. This project cannot guarantee account availability, rate limits, or continued compatibility.
