# ihme-cli

iCloud Hide My Email CLI. Go + Cobra.

## Build

```
go build -o ihme ./cmd/ihme
```

## Architecture

```
cmd/
  ihme/main.go          Entry point
  root/root.go           Root command, subcommand registration
  auth/                  login, status, logout (SRP + 2FA)
  list/                  List with --active/--inactive/--tag filters
  new/                   Generate + reserve in one shot
  view/                  View single address by ref
  edit/                  Edit label/note/tags
  copy/                  Copy address to clipboard
  lifecycle/             deactivate, reactivate, delete
  export/                CSV/JSON export with filters
  forward/               forward-to management

api/
  types.go               HmeEmail, SessionData, auth request/response types
  endpoints.go           URL constants, header maps
  client.go              HTTP client with cookie jar, auth/service request helpers
  auth.go                SRP auth flow: start -> federate -> init -> complete -> 2FA -> trust -> accountLogin
  session.go             Session persistence to ~/.config/ihme/session.json
  hme.go                 HME CRUD: list, generate, reserve, update, deactivate, reactivate, delete

internal/
  srp/                   SRP-6a with Apple modifications (NG_2048, SHA-256, NoUserNameInX)
  cmdutil/               GetClient helper, OutputResult (table/JSON/jq dispatch)

pkg/
  output/                Table, JSON, CSV, detail formatters
  tags/                  Parse/serialize #tag convention from note field
  resolver/              Universal ref resolver (anonymousId, email, label fuzzy match)
```

## Auth flow

Apple SRP via idmsa.apple.com (web auth path, not GSA):
1. GET  /appleauth/auth/authorize/signin — init session
2. POST /appleauth/auth/federate — submit email
3. POST /appleauth/auth/signin/init — SRP public key exchange
4. POST /appleauth/auth/signin/complete — SRP proof (M1/M2)
5. POST /appleauth/auth/verify/trusteddevice/securitycode — 2FA (if 409)
6. GET  /appleauth/auth/2sv/trust — trust token
7. POST setup.icloud.com/setup/ws/1/accountLogin — get webservices map

Password derivation: SHA256(password) -> PBKDF2(hash, salt, iterations, 32)
Protocol s2k_fo uses hex-encoded SHA256 instead of raw bytes.

## HME API

Base: {webservices.premiummailsettings.url}
- GET  /v2/hme/list
- POST /v1/hme/generate, reserve, updateMetaData, deactivate, reactivate, delete, updateForwardTo

## Design rules

- Every command supports --json and --jq
- <ref> resolves by anonymousId, email, or label (fuzzy)
- Errors to stderr, data to stdout
- Exit codes: 0 success, 1 error, 2 auth required
- Session stored at ~/.config/ihme/session.json (0600)
- Trust token enables 2FA-free re-login for ~30 days

## Reference implementations

- Go-iClient (github.com/Johnw7789/Go-iClient) — Go SRP + HME reference
- pyicloud / icloudpd — Python SRP + 2FA reference
- rclone iclouddrive — Go SRP production implementation
- Our browser extension — API surface and HME types
