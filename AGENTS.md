# AGENTS.md

Server that copies Google Health exercises into Garmin Connect, served with a small web UI.

## Layout

- `cmd/goomin` — cli (`serve` by default, `sync` for one run) and token wiring
- `server/` — the chi + oapi-codegen strict API: `server/server.gen.go` is generated from `schema/openapi.yaml` by `task generate`; never edit it by hand. Call `GetSpec()`, not the deprecated `GetSwagger()`
- `server/serve.go` — router, request-validation middleware and the React build; the page has no auth, run it behind a proxy and bind to loopback
- `google/` — Google Health API: auth URL, token exchange/refresh, exercise list and TCX export
- `garmin/` — Garmin Connect: SSO form login (with MFA), ticket exchange, TCX upload
- `syncer/` — the run itself: ledger, per-exercise isolation, 401 retry
- `state/` — one JSON file: tokens, upload ledger, log (`0600`, dir `0700`)
- `frontend/` — Vite+ (React, Tailwind, shadcn), built to `dist/` and embedded with `go:embed`; bun, not npm. `dist/.gitkeep` stays committed because `go:embed all:dist` needs the directory

## Rules

- One user, one process. Concurrency is a mutex plus the syncer's `running` flag; do not add a database.
- Never log tokens, passwords or full response bodies. Log one line per exercise.
- The ledger (`Uploaded`) is what prevents duplicates: record an id only when Garmin answers `uploaded` or `409`.
- Keep the 90-day window wider than the interval; Google's Testing-mode refresh token dies weekly.
- Garmin refuses TCX sports other than Running, Biking, Other: `RewriteSport` stays.
- The scheduler syncs once at startup, then every `--interval`, with no due-check: extra runs are harmless because the ledger dedups. Do not re-add a `due()` gate — with an exact interval the strict `>` made every tick skip.
- Production code lands with a test that was watched failing first; `task lint` and `task test` must be clean.
- The API lives in `schema/openapi.yaml`: change the spec, run `task generate`, then implement the strict handler. Do not add routes by hand.

## Config and env

- Flags are `--port`, `--state`, `--interval` only. The web listener is `:port`; keep the loopback decision in `compose.yaml`/`run:docker`, not in the cli.
- `PUBLIC_URL` (env, not flag) drives the Google redirect: `PUBLIC_URL/api/auth/google/callback`; default `http://127.0.0.1:8080`.
- `godotenv` reads `.env` from the process cwd: locally that is the repo root, in the container it is `/home/nonroot` (inside the volume — empty), so container deployment must pass env via `compose.yaml`'s `env_file: .env` or `--env-file`.
- The image is distroless `nonroot` (uid 65532). State must land somewhere that uid can write — compose uses a named volume, not `./data`.

## Commands

`task run`, `task sync`, `task generate`, `task test`, `task lint`, `task format`, `task build`, `task build:docker`, `task check:fe`, `task dev:fe`.

- `task build` and `task run` build the frontend first (`build:fe` runs `bun install --frozen-lockfile` then `bun run build`).
- Lint and format run through `go tool` (tool directives in go.mod); bumping golangci-lint needs an explicit `go get github.com/golangci/golangci-lint/v2@latest` + `go mod tidy`.
- `task test` caches; use `task test:clean` after changes that may hit stale test cache.
- `go test ./...` walks `frontend/node_modules/flatted/golang` as one extra (ignored) package — do not chase it.
