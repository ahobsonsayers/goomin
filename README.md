# health-connectd

Reads workouts from Google Health and uploads them to Garmin Connect as TCX, on a schedule, from a server.

Garmin has no OAuth for writing finished activities, so the sign-in is Garmin's own SSO form: the password goes to Garmin, only the tokens are stored.

## Run

```bash
cp .env.example .env     # fill in the Google OAuth client
task run                 # builds the frontend, then http://127.0.0.1:8080
```

Click **Connect Google**, then **Connect Garmin**. Sync runs every 12 hours, once at startup, and on **Sync now**.

## Layout

- `openapi.yaml` — the API; `task generate` writes `api/api.gen.go` (chi router + strict server)
- `google/`, `garmin/` — the two providers (Garmin: SSO form, MFA, ticket exchange, TCX upload)
- `syncer/` — ledger, per-exercise isolation, 401 retry
- `state/` — one JSON file: tokens, upload ledger, log
- `web/` — API handlers and static serving
- `frontend/` — Vite+ (React, Tailwind, shadcn), built to `frontend/dist` and embedded with `go:embed`

## Config

| Flag / env | Default | Meaning |
|---|---|---|
| `--state` | `state.json` | tokens, upload ledger and log |
| `--port` / `--bind` | `8080` / `127.0.0.1` | web listener |
| `--public-url` | `http://127.0.0.1:8080` | URL the browser uses |
| `--interval` | `12h` | sync period |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | — | from `.env` |
| `UI_PASSWORD` | — | basic auth for the page; set it on any public host |

The Google client is a **Web application** client with redirect URI `<public-url>/api/auth/google/callback`.

## Commands

`task run`, `task sync`, `task generate`, `task test`, `task lint`, `task format`, `task build`, `task build:docker`, `task check:fe`, `task dev:fe`.
