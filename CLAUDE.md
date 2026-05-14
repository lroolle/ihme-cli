# ihme-cli

iCloud Hide My Email CLI. Go + Cobra + Viper.

## Reference implementations

- `../reference/gh-cli/` — GitHub CLI (command hierarchy, --json/--jq, auth flow, extensions)
- `../reference/atlas-cli/` — Atlassian CLI (skill-native pattern, agent-first output, Go structure)
- `../worktree/icloud-hide-my-email-browser-extension/src/iCloudClient.ts` — our API layer (endpoints, types, auth flow)

## API endpoints

Base: `https://setup.icloud.com/setup/ws/1`
HME service: `{webservices.premiummailsettings.url}/v1/hme/`

- POST `/validate` — validate session, returns webservices map
- GET  `/v2/hme/list` — all addresses
- POST `/v1/hme/generate` — create candidate address
- POST `/v1/hme/reserve` — persist address with label/note
- POST `/v1/hme/updateMetaData` — edit label/note
- POST `/v1/hme/deactivate` — stop receiving
- POST `/v1/hme/reactivate` — resume receiving
- POST `/v1/hme/delete` — permanent removal
- POST `/v1/hme/updateForwardTo` — change forward-to email

## Auth

Apple SRP (Secure Remote Password) + 2FA. Reference: pyicloud, icloud-api (Rust).
Session stored in `~/.config/ihme/session.json`.

## Build

```
go build -o ihme ./cmd/ihme
```

## Design rules

- Every command supports `--json` and `--jq`
- `<ref>` arguments resolve by anonymousId, label (fuzzy), or email
- No interactive TUI — breaks agent integration
- Errors go to stderr, data goes to stdout
- Exit codes: 0 success, 1 error, 2 auth required
