# ihme-cli

iCloud Hide My Email CLI. Go + Cobra.

## Build

```
make              # build
make check        # vet + test + build
make cross        # cross-compile all platforms
```

## Architecture

```
cmd/
  ihme/main.go          Entry point (version from ldflags)
  root/root.go           Root command, subcommand registration
  auth/                  login (SRP + 2FA), status, logout
  list/                  List with --search/--active/--inactive/--tag/--sort
  new/                   Generate candidates, interactive pick, --address to reserve
  view/                  View single address by ref
  edit/                  Edit label/note/tags
  copy/                  Copy address to clipboard
  lifecycle/             deactivate, reactivate, delete (with --yes confirmation)
  export/                CSV/JSON export with filters
  forward/               forward-to management

api/
  types.go               HmeEmail, SessionData, SavedCookie, auth types
  endpoints.go           URL constants, header maps
  client.go              HTTP client, manual Cookie header (rclone pattern), mergeCookies
  auth.go                SRP auth: start -> federate -> init -> complete -> 2FA -> trust -> accountLogin
  session.go             Session persistence to ~/.config/ihme/session.json
  hme.go                 HME CRUD: list, generate, reserve, update, deactivate, reactivate, delete

internal/
  srp/                   SRP-6a (NG_2048, SHA-256, NoUserNameInX)
  cmdutil/               GetClient, OutputResult, ExactRefArg
  app/                   Application service: the six HME operations
                         shared by Cobra commands and agent tools
                         (both are adapters over it)
  agent/                 Embedded-agent adapter: BYOK config, six
                         in-process tools, scoped-consent gate,
                         renderer (see `ihme new --agent`)

pkg/
  output/                Table, JSON, CSV, detail formatters
  filter/                --active/--inactive/--tag/--search/--sort
  tags/                  Parse/serialize #tag convention
  resolver/              Universal ref resolver (ID, email, label fuzzy)
  agentkit/              Embeddable agent kernel (stdlib-only, never
                         imports ihme packages — see its README)

skill/
  SKILL.md               Operational procedure shared by external
                         agents (shell) and the embedded agent
                         (go:embed, invoked as a task turn)

examples/
  toyagent/              Minimal agentkit consumer (live smoke)
```

## Embedded agent

Two entry points, one adapter (internal/agent):

- `ihme new <label> --agent [--grant ask|auto]` — scoped: runs the
  SKILL.md procedure; pre-grants ONE reservation for the label and
  touching only addresses created this run; everything else prompts.
- `ihme agent [task]` (alias `ihme --agent`) — general: interactive
  REPL without args, one-shot with a task. NOTHING is pre-granted:
  every mutation (reserve/deactivate/edit) asks unless --grant auto.

BYOK: ~/.config/ihme/agent.json {model, baseUrl, apiKeyEnv, api,
effort} + ~/.config/ihme/.env (or OPENAI_MODEL/OPENAI_BASE_URL/
OPENAI_API_KEY). api selects the wire protocol: "completions"
(default) or "responses" — reasoning models (o-series, gpt-5.x)
reject function tools on /chat/completions and need "responses".
Rotation is capped at 3 generation rounds in the tool. Kernel
invariants and design: pkg/agentkit/README.md.

## Auth flow

Apple SRP via idmsa.apple.com (web auth, not GSA):
1. GET  /authorize/signin — init session
2. POST /federate — submit email
3. POST /signin/init — SRP key exchange
4. POST /signin/complete — SRP proof (M1/M2)
5. 2FA: SMS (PUT /verify/phone) or device push (PUT /verify/trusteddevice/securitycode)
6. GET  /2sv/trust — trust token
7. POST setup.icloud.com/accountLogin — get webservices map + cookies

Cookie handling: manual Cookie header from session cookie list (rclone pattern).
No Go cookie jar domain matching — cookies go to all service requests.
CN accounts: auto-fallback to setup.icloud.com.cn on 421.

## Session resume

1. Load session + cookies from disk
2. Try validate (uses persisted cookies, no Apple sign-in email)
3. If validate fails, fall back to accountLogin (triggers sign-in email)
4. Save updated session after resume

## Design rules

- Every command supports --json and --jq
- --json output includes hints with follow-up commands
- --help documents JSON response shapes
- <ref> resolves by anonymousId, email, or label (fuzzy)
- Errors include usage example and fix command
- Errors to stderr, data to stdout
- Exit codes: 0 success, 1 error, 2 auth required
- Session at ~/.config/ihme/session.json (respects $XDG_CONFIG_HOME), 0600
- --verbose/-v is global, logs all HTTP requests

## Release

goreleaser on tag push. Archives include LICENSE, README, SKILL.md.
