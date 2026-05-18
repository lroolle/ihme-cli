# Security

## Session storage

`ihme` stores session tokens and cookies at:
- macOS / Linux: `~/.config/ihme/session.json` (respects `$XDG_CONFIG_HOME`)
- Windows: `%AppData%\ihme\session.json`

Override the full path with `IHME_SESSION_PATH`.

File permissions are `0600` (owner read/write only).

**What's stored**: session token, trust token, scnt, session ID, cookies, webservice URLs.

**What's NOT stored**: Apple ID password, 2FA codes, or any credentials.

## Authentication

- SRP-6a protocol — password is never transmitted over the network
- 2FA via SMS or trusted device push
- Trust token enables 2FA-free login for ~30 days

## Reporting vulnerabilities

If you find a security issue, please email the maintainer directly instead of opening a public issue. Include steps to reproduce.

## Disclaimer

This tool uses Apple's undocumented iCloud web API. Apple may change or block access at any time. The authors are not responsible for account restrictions or other consequences.
