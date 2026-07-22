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

Interaction design: one input authority (asker) per session. The
interactive REPL is an inline Bubble Tea UI with Bubbles text input:
streamed work is summarized as status rows, reasoning/tool JSON stays
out of the transcript, questions get a dedicated input, and consent is
a three-choice control (allow once / deny / always this run). Drafts
typed while the model works are preserved for the next turn and never
become phantom prompt answers. One-shot runs retain a cooked-mode text
fallback with drain-and-reprompt consent.

BYOK: ~/.config/ihme/agent.json {model, baseUrl, apiKeyEnv, api,
effort} + ~/.config/ihme/.env (or OPENAI_MODEL/OPENAI_BASE_URL/
OPENAI_API_KEY). api defaults to "auto": guess the wire protocol
from the model family (gpt-5*/o1/o3/o4/codex -> responses, else
completions), flip automatically on the endpoint's misroute signal,
and persist the discovery to agent.json — the wrong first call
happens at most once per model. Pin "completions"/"responses" to
disable detection.
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
2. If validatedAt is fresh (< 15 min), skip validation entirely —
   commands start straight on saved cookies
3. Otherwise validate (uses persisted cookies, no sign-in email).
   Failures are CLASSIFIED: only a definitive rejection (401/403/450
   or success=false) falls back to accountLogin; transport trouble
   (timeouts, 5xx, proxy flaps, 421 routing hiccups) gets one quiet
   retry, then surfaces as "iCloud temporarily unreachable — your
   session is probably still valid" WITHOUT touching accountLogin.
   421 is NOT expiry: Apple returns it for routing/rate pressure on
   valid sessions (the CN-region case is auto-retried separately).
4. accountLogin (triggers sign-in email) only on real rejection; its
   own transient failures also report as unreachable, not expired
5. Save updated session (cookies + validatedAt) after resume

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
