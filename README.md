# goomin

Reads workouts from Google Health and uploads them to Garmin Connect as TCX, on a schedule, from a server.

Garmin has no OAuth for writing finished activities, so the sign-in is Garmin's own SSO form: the password goes to Garmin, only the tokens are stored.

## Run

```bash
cp .env.example .env     # fill in the Google OAuth client
task run                 # builds the frontend, then http://127.0.0.1:8080
```

Click **Connect Google**, then **Connect Garmin**. Sync runs every 12 hours, once at startup, and on **Sync now**.

## Layout

- `schema/openapi.yaml` — the API; `task generate` writes `internal/server/server.gen.go` (chi router + strict server + request validation)
- `internal/google/`, `internal/garmin/` — the two providers (Garmin: SSO form, MFA, ticket exchange, TCX upload)
- `internal/syncer/` — ledger, per-exercise isolation, 401 retry
- `internal/state/` — one JSON file: tokens, upload ledger, log
- `internal/server/` ( — API handlers served under `/api`, plus the static build
- `frontend/` — Vite+ (React, Tailwind, shadcn), built to `frontend/dist` and embedded with `go:embed`

## Config

| Flag / env | Default | Meaning |
|---|---|---|
| `--state` | `state.json` | tokens, upload ledger and log |
| `--port` | `8080` | web listener; compose.yaml binds it to loopback |
| `--interval` | `12h` | sync period |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | — | from `.env` |
| `PUBLIC_URL` | `http://127.0.0.1:8080` | URL the browser uses; redirects are `PUBLIC_URL/api/auth/google/callback` |

The Google client is a **Web application** client with redirect URI `<public-url>/api/auth/google/callback`.

There is no built-in password: the page has no auth, so put an OAuth proxy (or your own basic-auth reverse proxy) in front and only expose the port on loopback. `compose.yaml` publishes `127.0.0.1:8080:8080` for that reason.

## Commands

`task run`, `task sync`, `task generate`, `task test`, `task lint`, `task format`, `task build`, `task build:docker`, `task check:fe`, `task dev:fe`.
