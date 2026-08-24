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
  memorycmd/             Inspect the agent memory graph (path, search, graph, card)
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
  agent/                 Embedded-agent adapter: BYOK config,
                         in-process tools, scoped-consent gate,
                         renderer (see `ihme new --agent`); also the
                         harness side (`--via`, via.go) and the MCP
                         server (mcpserve.go) for `ihme mcp`
  acp/                   Minimal Agent Client Protocol client (v1
                         subset, hand-rolled): ihme as the HARNESS
                         spawning claude-code/codex/opencode as the
                         model provider — see `ihme agent --via`
  memory/                Agent memory: a Logseq-style markdown graph
                         (journals/, pages/, flashcards). Plain files,
                         no DB — see its package doc

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
  SKILL.md procedure; pre-grants ONE reservation for that label and
  touching only addresses created this run; everything else prompts.
- `ihme agent [task]` (alias `ihme --agent`) — general: interactive
  TUI without args, one-shot with a task; nothing is pre-granted.

A third mode inverts the provider: `ihme agent --via codex|claude|
opencode "<task>"` harnesses a full coding agent (its subscription
auth, its models — no BYOK key) as the brain. ihme is the ACP
client (internal/acp); the guest gets the HME operations back as
MCP tools by re-invoking this binary as `ihme mcp`. The consent
gate does NOT move to the guest: it runs inside the MCP child and
its cards travel over a unix socket to the harness terminal
(consentsock.go) — verified live that guest permission layers
(claude-agent-acp) execute mutating MCP tools without asking, so
ours is the only real gate. Tool physics (rationale floor, caps,
journaling) are unchanged — they live in tools.go, not the loop.

Code is the source of truth for the behavior; the load-bearing
comments live where the behavior lives:

- internal/agent/tui.go — interactive session: input authority,
  consent card, typed-reply redirect, reasoning status line
- internal/agent/gate.go — the scoped-consent policy
- internal/agent/interact.go — consent protocol (y/N/a/reply)
- internal/agent/config.go, auto.go — BYOK config keys and
  wire-protocol auto-detection
- internal/agent/run.go — system prompt, renderer, one-shot runs
- internal/agent/via.go — the --via harness: guest resolution,
  session/prompt loop, update rendering, consent socket wiring
- internal/agent/mcpserve.go — `ihme mcp`: the tool layer over MCP
  stdio, gate in front of every call
- internal/agent/consentsock.go — consent cards across the process
  boundary (why: the guest's permission layer never asks)
- internal/acp/client.go — the ACP v1 client subset and its types
- internal/agent/memtool.go — memory glue: the recall_memory /
  remember tools, the <memory> continuity block injected at session
  start, and the journal+page write made at reserve time
- internal/memory/memory.go — the graph store itself (journals,
  pages, flashcards, derived link graph); package doc has the design
- skill/SKILL.md — the operational procedure. RUNTIME PROMPT
  CONTENT (go:embed, invoked as a task turn): whenever tool schemas
  in tools.go change, its embedded-agent section must change with
  them, or the model reads two contracts.
- pkg/agentkit/README.md — kernel contract and invariants

Design rejections and their reasons: TASTE.md. When something needs
explaining, prefer (in order) a code comment at the behavior, a scar
in TASTE.md, or a where-to-look line here. Do not re-grow prose that
restates tui.go — it drifted within hours the last time; this
section has been composted once already.

## Auth flow

Apple SRP via idmsa.apple.com (web auth, not GSA):
1. GET  /authorize/signin — init session
2. POST /federate — submit email
3. POST /signin/init — SRP key exchange
4. POST /signin/complete — SRP proof (M1/M2)
5. 2FA: SMS (PUT /verify/phone) or device push (PUT /verify/trusteddevice/securitycode).
   Since ~mid-2026 Apple answers the securitycode POST with 409 even
   for a VALID code — the acceptance signal is the fresh
   X-Apple-Session-Token header, not the status (rclone#9488). Do
   not "restore" a strict 2xx check there.
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
6. A service host can still answer 401 inside that window — the
   mail-domain host holds its own cookies, so /validate passing is
   not its promise. api/hme.go recovers once: accountLogin (NOT
   validate, which already lied), rebuild the URL from the fresh
   webservices map and dsid, replay the call. A rejected call
   changed nothing on Apple's side, so replaying is safe even for
   mutations. Recovery is one shot; a session Apple keeps refusing
   is expired, and retry loops against auth endpoints get accounts
   rate limited. Fresh cookies go back to disk through
   Client.OnSessionUpdate.
7. clientId is generated once and persisted in the session — one
   installation, not a new stranger every invocation.

## Design rules

- Every command supports --json and --jq
- --json output includes hints with follow-up commands
- --help documents JSON response shapes
- <ref> resolves by anonymousId, email, or label (fuzzy)
- Errors include usage example and fix command; cmdutil.Explain is
  the single place that turns an error into user text (what
  happened / Cause / Fix) and cmdutil.ExitCode the single place
  that maps it to 0/1/2
- Error text never carries dsid or clientId (api.redactURL strips
  the query) — pasteable into an issue
- Errors to stderr, data to stdout
- Exit codes: 0 success, 1 error, 2 auth required
- Session at ~/.config/ihme/session.json (respects $XDG_CONFIG_HOME), 0600
- --verbose/-v is global, logs all HTTP requests

## Release

goreleaser on tag push. Archives include LICENSE, README, SKILL.md.
