# AGENTS.md

Server that copies Google Health exercises into Garmin Connect, served with a small web UI.

## Layout

- `cmd/health-connectd` — cli (`serve` by default, `sync` for one run) and token wiring
- `api/` — generated from `openapi.yaml` by `task generate`; never edit by hand
- `google/` — Google Health API: auth URL, token exchange/refresh, exercise list and TCX export
- `garmin/` — Garmin Connect: SSO form login (with MFA), ticket exchange, TCX upload
- `syncer/` — the run itself: ledger, per-exercise isolation, 401 retry
- `state/` — one JSON file: tokens, upload ledger, log (`0600`, dir `0700`)
- `web/` — strict API handlers, static serving of the React build, basic auth when `UI_PASSWORD` is set
- `frontend/` — Vite+ (React, Tailwind, shadcn), built to `dist/` and embedded with `go:embed`; bun, not npm
- `worker/` — interval loop

## Rules

- One user, one process. Concurrency is a mutex plus the syncer's `running` flag; do not add a database.
- Never log tokens, passwords or full response bodies. Log one line per exercise.
- The ledger (`Uploaded`) is what prevents duplicates: record an id only when Garmin answers `uploaded` or `409`.
- Keep the 90-day window wider than the interval; Google's Testing-mode refresh token dies weekly.
- Garmin refuses TCX sports other than Running, Biking, Other: `RewriteSport` stays.
- Production code lands with a test that was watched failing first; `task lint` and `task test` must be clean.
- The API lives in `openapi.yaml`: change the spec, run `task generate`, then implement the strict handler. Do not add routes by hand.

## Commands

`task run`, `task sync`, `task generate`, `task test`, `task lint`, `task format`, `task build`, `task build:docker`, `task check:fe`, `task dev:fe`.

`task build` and `task run` build the frontend first (`build:fe` runs `bun install --frozen-lockfile` then `bun run build`).
