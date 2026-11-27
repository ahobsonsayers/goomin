# Health Connect ⇄ Garmin: what is actually possible (checked 2026-11-25)

Goal: phone-side app (Flutter, Android) that moves health data between Google's health stack
(Health Connect / Google Health) and Garmin Connect.

## The one hard fact

**Garmin Connect is read-only outward. It never reads data from Health Connect or Apple Health.**
Garmin's own support page: *"Please note that this is a one-way transfer; Garmin will not read any
data from Health Connect."* — https://support.garmin.com/en-US/?faq=JToBEy0jfe6pIygark2Ui5

Garmin's platform rules (support article):
- One-Way Sync: most third-party services only receive data from Garmin Connect.
- Two-Way Sync: some services can send data in, but supported data types vary.
- No Data Forwarding: Garmin Connect does not forward data received from one third party to another
  (so a Zwift/TrainerRoad ride in Garmin does not continue on to Strava).

Nothing on the consumer side bypasses this. Any app that "syncs to Garmin" with your account
password is using Garmin's private mobile API, against the ToS, and Garmin revokes partner licences
for that. Health Sync states explicitly it refuses to do it for that reason.

## Garmin developer access (relaunched 21 Oct 2026)

| Tier | Cost | Delivery | Data | Write? |
|---|---|---|---|---|
| Personal API key (from Garmin Connect web settings, shown once) | free | pull only | activity JSON + .FIT, health JSON, women's health JSON | no |
| Starter (business app) | free | push only | same, no historical data, limited user count | not for individuals |
| Enterprise | $1,000 entry from 1 Apr 2027, credited to per-active-user bill (DC Rainmaker) | push or ping/pull | + golf, 1 month+ history, unlimited users | subject to agreement |

- Overview: https://developer.garmin.com/gc-developer-program/
- Health API: https://developer.garmin.com/gc-developer-program/enterprise/health-api/
- Agreement §1(b)/§4.5 does allow "End Users to upload End User Data to their Garmin Connect
  accounts via Licensee Applications" — so writing is a contract matter, not a technical one:
  https://www8.garmin.com/GARMINCONNECTDEVELOPERPROGRAMAGREEMENT/GARMINCONNECTDEVELOPERPROGRAMAGREEMENT_EN.pdf

For a personal/hobby app: all three tiers are useless for writing. Read the personal key for
pulling your own Garmin data, nothing more.

## How Health Sync (the commercial app) solves "sync to Garmin"

It doesn't. It writes **FIT / TCX / GPX / CSV files to Google Drive** (Huawei Drive on Huawei phones)
and you import them by hand:
1. https://connect.garmin.com → cloud-upload icon ("Import data")
2. Upload the FIT file for each activity.
Weight: FIT files to Drive for manual import, or Google Health → MyFitnessPal → Garmin.

Source: https://healthsync.app/f-a-q/ → "I want to sync to Garmin Connect", and
https://healthsync.app/about/ ("Synchronize health and fitness data", export section).

## What Garmin pushes out to Health Connect

Android 14 and above only, after each successful device sync, one-way.

Activity: active + total calories, cycling pedal cadence, distance, elevation gained, heart rate,
speed, steps, swimming strokes.
Wellness: body fat, calories burned, floors climbed, heart rate, sleep stages, steps + step distance,
weight.

Never shared: HRV, Body Battery, stress, VO2 max, respiration, SpO2, skin temperature,
Training Readiness, intensity minutes. Also no GPS track to Apple Health.
Source: https://www.sensai.fit/blog/apps-that-work-with-garmin (checked 4 Nov 2026), Garmin FAQ above.

## Google side, 2026

- Google Fit APIs: supported only until **end of 2026**; migrate to Health Connect (on-device) or the
  Google Health API (cloud). https://developer.android.com/health-and-fitness/health-connect/migration/fit
- Fitbit app is now the **Google Health app** (rebrand from 19 May 2026). It aggregates through
  Health Connect on Android and Apple Health on iOS, plus direct partner links.
  https://support.google.com/googlehealth/answer/14236613
- Legacy Fitbit Web API + Fitbit OAuth: switched off **2 Nov 2026** (HTTP 410 Gone). Replaced by the
  **Google Health API**: `https://health.googleapis.com`, `users/{id|me}/dataTypes/{type}/dataPoints:*`,
  Google OAuth 2.0, all scopes classed *Restricted* → privacy + security review before production.
  https://developers.google.com/health/about and https://developers.google.com/health/migration/api-specifications
- Google Health API is read **and write** for many data types (create/patch dataPoints), but it writes
  into Google Health, not into Garmin.

## Consequence for our app

Reading is easy and free; writing to Garmin is not.

Read (pick one):
- **Health Connect directly** (`health` package, already in pubspec). On-device permissions, no OAuth,
  no server, no review, no Play Store project declaration for personal sideloaded debug builds.
  Garmin's exported data lands here automatically.
- Google Health API only if we need Fitbit/Pixel cloud data or webhooks — costs an OAuth client,
  restricted-scope review, and a backend.

Write to Garmin (compliant options only):
1. **Local FIT generation + manual import.** Generate a FIT activity file from a Health Connect
   `ExerciseSessionRecord` (+ HR/route samples) and let the user upload it at connect.garmin.com.
   Same route Health Sync uses. Needs: Garmin FIT SDK or a Dart FIT writer (verify: `fit_tool` on pub.dev).
2. Share sheet / Google Drive upload to remove the cable-and-desktop step.
3. WebView automation of connect.garmin.com import (login + file input) — works, but scraping a
   consumer site is the fragile option; still the user's own account and own file.
4. Contract route (Garmin Starter/Enterprise) — out of scope for a personal app.

Don't: store the user's Garmin password, hit Garmin SSO/mobile endpoints. ToS violation, and Garmin
actively polices it.

## Apps that *do* write to Garmin with username + password (for reference)

Garmin's block on this is contractual (ToS + partner licence), not technical. Several shipping apps do it anyway.

| App | Platform | Mechanism | Source available |
|---|---|---|---|
| RunGap | iOS | email+password sign-in, "Write Only" mode, uploads .fit, optional Garmin "device identity" so the upload looks like a device, skips dupes by start time | closed |
| SyncMyTracks | Android | email+password per service, exports/imports GPX or TCX, credentials encrypted on device, added "Garmin Connect security code verification" (MFA) | closed |
| Nabe | web | claims write + device identity | closed |
| tapiriik | web, open source | GitHub `cpfair/tapiriik`, `services/GarminConnect/garminconnect.py`: `AuthenticationType.UsernamePassword`, form login to `/modern`, FIT + multipart POST, then PUT activity metadata | yes — but Garmin revoked its upload access |
| FitnessSyncer | web | read only; states "You cannot publish any data to this service at this time" | closed |
| HealthFit | iOS | exports FIT/GPX/CSV only, no write | closed |

The historical upload call in tapiriik (readable reference):

```python
session.post("https://connect.garmin.com/modern/proxy/upload-service/upload/.fit",
             files={"data": ("x.fit", fit_file)}, headers={"nk": "NT"})
# -> {"detailedImportResult": {"successes": [{"internalId": 123}], "failures": []}}
# then PUT https://connect.garmin.com/proxy/activity-service/activity/123  (name, type, private)
```

Failure strings worth knowing: `"Duplicate activity"` (harmless), `"The user is from EU location, but
upload consent is not yet granted or revoked"` — EU accounts must grant upload consent in Garmin
Connect settings before any upload works.

Current (2026) state of the credential route — **it is broken at the HTTP level**:

- `garth` (Python, matin) is **deprecated**, final release v0.8.0 on 2026-03-28, PyPI classifier
  `Development Status :: 7 - Inactive`. Reason quoted from its README: *"Garmin changed their auth flow,
  breaking the mobile auth approach that Garth depends on."* Existing saved OAuth1 sessions keep
  working until the token expires (~1 year); **new logins fail**.
  https://github.com/matin/garth/discussions/222
- What Garmin actually did (diagnosis in PR matin/garth#225): `/mobile/api/login` returns 400
  (endpoint disabled), `/oauth-service/oauth/preauthorized` returns 401 (disabled), and Cloudflare
  bot detection 403s `requests`, `httpx` and even `curl_cffi` with Chrome TLS fingerprints. Replaying
  cookies extracted from a browser also 403s. Only `page.evaluate(fetch(...))` inside a real browser
  returns 200.
- Successor: `garminconnect` (cyberjunky) 0.3.20, 2026-10-23, dropped `garth` at 0.3.0 and signs in
  through Garmin's mobile SSO itself. Old garth token files cannot be migrated.
  **Correction (source read 2026-11-25): it does NOT use a browser.** See the next section.
- Everything downstream is affected: Home Assistant's Garmin integration, GarminDB, garminexport,
  garmin-connect-export, most MCP servers.

**Why the wall is beatable:** the fence is the *login*, not the API. `connectapi.garmin.com` answers
plain HTTP and only rejects on auth (`403 ForbiddenException`, no Cloudflare). And the login itself
is beatable without a browser by faking a real client's TLS fingerprint — which is what
`garminconnect` 0.3.20 does. See the next section for the exact numbers.

Verified upload endpoints: `POST connectapi.garmin.com/upload-service/upload` (form field `file`, FIT/
GPX/TCX) and `POST .../upload-service/upload/{fit|gpx|tcx}` with Garmin-mobile import headers.
Web equivalent, as in tapiriik: `connect.garmin.com/modern/proxy/upload-service/upload/.fit`.

Pattern worth copying regardless of who invented it:
1. Never persist the password — exchange it for a token once, keep only the token.
2. Treat MFA as a normal step, not an error (SyncMyTracks ships a security-code screen).
3. Write-only mode: don't read back, don't touch existing activities.
4. Dedupe on activity start time before uploading.
5. Garmin ignores steps embedded in an activity (they are not added to daily totals).

Risk: ToS breach, account flags/lockout, and the API changes without notice. Fine for a personal
tool on your own account; not shippable.

## SyncMyTracks 3.14.14: the actual working flow (decompiled 2026-11-25)

Obtained via the geocache `gplaydl` setup: `com.syncmytracks.free` (Play does not let a non-purchased
account fetch `com.syncmytracks`), `jadx -d scratch/ref/decompiled`. Note the free build is the *same*
app with a trial gate, so the sync code is identical.

Source: `scratch/ref/decompiled/sources/com/syncmytracks/trackers/Garmin.java` (1015 lines, not
obfuscated). This is the live 2026 flow and it is **plain HTTP with a cookie jar, no browser, no TLS
impersonation, no delays**. garth was wrong about needing a browser — see the note at the end.

### Login

1. GET the SSO **embed widget**, keep cookies:
   `https://sso.garmin.com/sso/signin?service=https%3A%2F%2Fsso.garmin.com%2Fsso%2Fembed&source=…&redirectAfterAccountLoginUrl=…&gauthHost=https%3A%2F%2Fsso.garmin.com%2Fsso&locale=en&id=gauth-widget&cssUrl=https%3A%2F%2Fstatic.garmincdn.com%2Fcom.garmin.connect%2Fui%2Fcss%2Fgcm-sso-theme-v1.8.css&clientId=GarminConnectMobileAndroid&consumeServiceTicket=true&embedWidget=true&mobile=true&showPassword=true&…`
   Parse `input[name=_csrf]`.
2. POST the same URL, form body `username`, `password`, `embed=true`, `_csrf`, headers
   `origin: https://sso.garmin.com` and UA
   `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.0.0 Safari/537.36`.
3. Success page carries `var response_url = "https:\/\/sso.garmin.com\/sso\/embed?ticket=ST-…"`.
   Unescape `\/` → `/`, take the `ticket` query param, GET the ticket URL (same cookie jar).
4. Exchange the ticket at `https://diauth.garmin.com/di-oauth2-service/oauth/token`:

   ```
   Authorization: Basic R0FSTUlOX0NPTk5FQ1RfTU9CSUxFX0FORFJPSURfREk6   # = "GARMIN_CONNECT_MOBILE_ANDROID_DI:"
   client_id     = GARMIN_CONNECT_MOBILE_ANDROID_DI
   service_ticket= ST-…
   grant_type    = https://connectapi.garmin.com/di-oauth2-service/oauth/grant/service_ticket
   service_url   = https://sso.garmin.com/sso/embed
   ```
   → `{access_token, refresh_token}`.
5. Refresh later with `grant_type=refresh_token` + `refresh_token`, same Basic header, same client_id.
   Token liveness check: `GET https://connectapi.garmin.com/userprofile-service/userprofile/user-settings`
   with `Authorization: Bearer …`; 401 means dead.
6. **MFA** is detected in the login response by `enterMfaCode` / `setupMfaRequiredView` / `input#mfa-code`.
   Parse `var mfaMethod="…"`, `var codeSentTo="…"` (shown to the user), and `input[name=_csrf]`, then
   POST `/sso/verifyMFA/loginEnterMfaCode?<same query string>` with
   `mfa-code`, `embed=true`, `_csrf`, `fromPage=setupEnterMfaCode`. The reply has `response_url` again.

Session state lives in a persistent `CookieManager` behind `JavaNetCookieJar` (Tracker.java:118), which
is why restarting the app does not force a re-login. Tokens are stored per (user, password) in prefs.

### Upload

```java
POST https://connectapi.garmin.com/upload-service/upload/.tcx   // or .gpx
Authorization: Bearer <access_token>
referer: https://connect.garmin.com/modern/import-data
multipart/form-data: file=<name>.tcx  (application/octet-stream)
```

Response JSON `detailedImportResult.successes[0].internalId`, `failures[]`. Error strings to match:

- `"content":"Duplicate Activity."` → already imported
- `"The user is from EU location…"` / `revoked consent for uploading files` → account must grant
  upload consent in Connect settings

TCX massaging before upload (`postApiFile`): force `<Activity Sport="…">` to `Running` unless it is
Running/Biking/Other, and if the file has no `<Creator>`/`<Author>`, inject

```xml
<Author xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="Application_t">
  <Name>Connect Api</Name>
  <Build><Version><VersionMajor>0</VersionMajor><VersionMinor>0</VersionMinor>
  <BuildMajor>0</BuildMajor><BuildMinor>0</BuildMinor></Version></Build>
  <LangID>en</LangID><PartNumber>000-D0000-00</PartNumber>
</Author>
```

That `<Author>` is the "looks like a device/api upload" trick — it makes Garmin accept and attribute
the file instead of rejecting it as third-party.

### Weight (the only non-activity data Garmin accepts)

```java
GET  https://connectapi.garmin.com/weight-service/weight/latest?date=YYYY-MM-DD&ignorePriority=true
POST https://connectapi.garmin.com/weight-service/user-weight      // x-http-method-override: PUT
     {"value": 78.4, "unitKey": "kg", "timestampGMT": "YYYY-MM-DDTHH:mm:ss.0"}
```
It also has a FIT weight fallback: a FIT file with `WeightScaleMesg` POSTed to
`/upload-service/upload/.fit` — same multipart call as activities.

### Why garth died and this did not

garth posted credentials to `/mobile/api/login`, which Garmin disabled. SyncMyTracks posts to the
**embed widget** SSO form, which is still live, and keeps the resulting cookies. No TLS fingerprint
games are needed because OkHttp from a real phone looks like a browser, and the embed flow is not
behind the portal's Cloudflare challenge.

### Auth choice: there is no OAuth for writing into Garmin

Garmin's OAuth (developer program) covers *reading* a user's data and pushing workouts/courses to
devices. It has no scope for uploading activities into the account, so "connect with Garmin OAuth" is
not an option for this app. The two real choices:

| | Embed form (SyncMyTracks style) | WebView on Garmin's page |
|---|---|---|
| who sees the password | our code, once | Garmin only |
| MFA | we parse `mfaMethod`/`codeSentTo`, own dialog | Garmin's own screen |
| failure modes | form markup changes | WebView blocked/blank, cookie extraction breaks |
| extra deps | `dio`, `cookie_jar` | `webview_flutter` |
| ends the same way | `ticket` → `diauth` token exchange | identical |

Either way the password is used **once** and discarded: only `access_token` + `refresh_token` are
persisted (`flutter_secure_storage`). Refresh with `grant_type=refresh_token`; when Garmin revokes it,
ask for the password again rather than storing it.

## What this means for the Flutter app

Everything above is ordinary form POSTs + multipart, so `dio` + `dio_cookie_manager`/`cookie_jar`
does it. **No WebView, no FFI, no libcurl.** Earlier "you need a browser" advice is dead — delete it
from your head. The only friction is the long query string (copy it verbatim) and cookie persistence.

## garminconnect 0.3.20: the alternative recipe (read from source, not run)

Source: `pip download garminconnect --no-deps --no-binary :all:`, extracted at
`/tmp/opencode/gcsrc/garminconnect-0.3.20/`. This is the only maintained client after garth died, and
it is HTTP-only: **no browser anywhere**. Read 2026-11-25; not yet executed against a live account.

Login (`garminconnect/client/sso.py`) is a 5-strategy chain, each tried in order, auth errors stop it:

1. mobile iOS + `curl_cffi` with TLS impersonation (`safari_ios`, `safari`, `chrome120`)
2. mobile iOS + plain `requests`
3. SSO embed-widget HTML form flow (different rate-limit bucket, 3–8 s pause)
4. portal web + `curl_cffi` with a **10–20 s** anti-WAF pause
5. portal web + plain `requests`

The 10–20 s delay is not politeness, it is the fix: Cloudflare flags a fast GET→POST pair as a bot.

| Constant | Value |
|---|---|
| `IOS_SSO_CLIENT_ID` | `GCM_IOS_DARK` |
| `IOS_SERVICE_URL` | `https://mobile.integration.garmin.com/gcm/ios` |
| `MOBILE_SSO_CLIENT_ID` | `GCM_ANDROID_DARK` |
| `MOBILE_SSO_SERVICE_URL` | `https://mobile.integration.garmin.com/gcm/android` |
| `PORTAL_SSO_CLIENT_ID` | `GarminConnect` |
| `PORTAL_SSO_SERVICE_URL` | `https://connect.garmin.com/app` |
| `DI_TOKEN_URL` | `https://diauth.garmin.com/di-oauth2-service/oauth/token` |
| `DI_GRANT_TYPE` | `https://connectapi.garmin.com/di-oauth2-service/oauth/grant/service_ticket` |
| `DI_CLIENT_IDS` | `GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2`, `..._2024Q4`, `..._DI`, `..._IOS_DI` |

API identity headers the mobile app sends, and that the API tier expects:

```
User-Agent: GCM-Android-5.23
X-Garmin-User-Agent: com.garmin.android.apps.connectmobile/5.23; ; Google/sdk_gphone64_arm64/google; Android/33; Dalvik/2.1.0
```

MFA is a first-class branch, not an error: `sso.py` raises `_MFARequired`, and the caller either
supplies a `prompt_mfa` callback or calls with `return_on_mfa=True`, gets
`("needs_mfa", client_state)`, then `resume_login(client_state, mfa_code)`.

Uploads, `garminconnect/api/activities.py`:

```python
# api/base.py:295
self.garmin_connect_upload = "/upload-service/upload"

# upload_activity(): plain upload
client.post("connectapi", "/upload-service/upload", files={"file": (name, fh)}, api=True)

# import_activity(): same file, but marked as an import so Garmin does not
# re-export it to Strava, and it is not treated as a device-synced activity
client.post("connectapi", f"/upload-service/upload/{ext}",
            files={"file": (name, fh, "application/octet-stream")},
            headers={"NK": "NT", "origin": "https://sso.garmin.com",
                     "User-Agent": "GCM-iOS-5.7.2.1"}, api=True)
# 409 == "Activity already exists" -> dedupe hit, treat as success
```

Allowed formats: FIT, GPX, TCX.

### Flutter note

This variant needs TLS impersonation (`curl_cffi`-style handshake), which Dart cannot do without a
native libcurl. Ignore it: the SyncMyTracks flow above is plain HTTP and does everything we need.

## PC pipeline with no Python

Pull side is plain REST. Push side needs a real browser — that is the only hard requirement.

1. **Create the Google Cloud project in "Personal" user type.** One-time choice, irreversible
   (an External/Internal project cannot be switched later). Personal mode: authorization limited to
   the single Google Account that created the app, no verification, no unverified-app screen, no
   100-user cap, and **no 7-day refresh-token expiry**. Testing mode would expire the refresh token
   every 7 days, which breaks any cron job.
   https://support.google.com/cloud/answer/15549945
2. **Pull with curl or Node.** `POST https://oauth2.googleapis.com/token` (refresh token grant) →
   `GET https://health.googleapis.com/v4/users/me/dataTypes/{type}/dataPoints`. Save the refresh token
   once, cron the rest. ~20 lines of bash.
3. **Push with `npx playwright`.** `npx playwright codegen connect.garmin.com` records the login and
   the "Import data" clicks; replay the recording with the file path parameterised. Node, not Python.
   A Tampermonkey userscript on connect.garmin.com is the zero-install variant.
4. **Skip the FIT SDK.** The web importer accepts **TCX** and **GPX** as well as FIT, and both are
   plain XML/GPX you can template in any language. FIT only matters if you need laps, sessions or
   developer fields.
5. **Hard scope limit:** file import carries **activities** (and weight via FIT) only. Steps, sleep,
   all-day heart rate, floors cannot be uploaded this way by any script.

## Live probe of Garmin endpoints (2026-11-25, plain curl, no cookies)

Useful because it separates "needs auth" from "bot-blocked":

| URL | Result |
|---|---|
| `https://sso.garmin.com/sso/signin?service=...` (legacy form) | 200, real `<form id="login-form">` with `username`, `password`, `_csrf` |
| `https://connect.garmin.com/modern/` | 302 → `/app/` (cloudflare headers, but a plain redirect, not a challenge) |
| `https://connect.garmin.com/app/` | 302 → `/signin/?service=...`, tiny Astro shell |
| `https://sso.garmin.com/portal/sso/en-US/sign-in?clientId=GarminConnect&service=...` | **403 to curl** — this is the bot wall |
| `https://connectapi.garmin.com/userprofile-service/socialProfile` | 403 JSON `{"message":"HTTP 403 Forbidden","error":"ForbiddenException"}` — API-level, **no Cloudflare** |

Current web sign-in flow, read out of `connect.garmin.com/_astro/ssoUtil.CYwBj3bH.js` and
`signin...js`: the SPA redirects to
`https://sso.garmin.com/portal/sso/{locale}/sign-in?clientId=GarminConnect&service=<origin>/app`.
Host/clientId table: `connect.garmin.com` → `sso.garmin.com`, clientId `GarminConnect`.

Implication: `connectapi` is reachable over plain HTTP and only rejects on auth; it is the SSO that
fences off non-browsers. Any client that already holds a valid token can call it without a browser.

## Other numbers worth keeping

- Health Connect activity sessions: active/moving time is often wrong (total duration reported as
  active time). Health Sync claims to fix this; we would have to compute it from HR samples.
- Health Connect GPS: permission is per-activity, not per-app.
- Background reads: Health Connect supports background read since 2025 (`PermissionController` +
  background read permission), but most consumer apps only refresh on open.
- Health Sync: 1 week trial, one-time licence or 6-month subscription; historical sync in batches,
  max one sync per 15 minutes.
- Garmin historical limits when Garmin is the *source*: 5 years of activities, 2 years of other data.

## Open questions for the build

1. Direction is settled by the user: **Google Health → Garmin**, read side = Google Health API, not
   Health Connect.
2. Login transport is settled: the user chose **WebView on Garmin's own sign-in page**, so the password
   never touches our code and Garmin handles MFA itself. Only the DI `access_token` + `refresh_token`
   are kept (`flutter_secure_storage`). Built; see `lib/garmin/`.
3. Do we need activity sessions with GPS, or just daily wellness (steps/sleep/HR/weight)?
   Note the hard limit: file import carries activities and weight only — steps/sleep/all-day HR
   cannot be uploaded at all.
4. Is manual FIT import acceptable, or does the WebView-upload shortcut have to be automated?

## Health Sync 1212 (`nl.appyhapps.healthsync`), decompiled 2026-11-25

Not obfuscation-free: `com.pairip.VMRunner` (Play PairIP) hides part of the logic, and the classes are
renamed into `defpackage`. What is still readable is the whole network layer.

- **It is not a Garmin uploader.** The only Garmin hosts it talks to are `garmin.com/oauth2`,
  `apis.garmin.com/wellness-api/rest/user/id` and `/user/permissions` — i.e. the Garmin **Health API**
  (read from Garmin, as a registered partner). Nothing calls `upload-service`. It writes FIT/TCX/GPX
  and leaves them in Drive for the user to import. Same conclusion as the FAQ, now from code.
- **Google Health read side is the Google Health v4 REST API**, called with a bearer token:
  - `https://health.googleapis.com/v4/users/me/dataTypes/{type}/dataPoints?filter=…&pageSize=5000`
    (+ `&pageToken=` paging), types seen: `steps`, `weight`, `heart-rate`, `exercise`.
  - `…/dataTypes/heart-rate/dataPoints:rollUp` for aggregates.
  - `…/v4/users/me/identity` to identify the account.
  - Writes exist too: `…/dataTypes/exercise/dataPoints` (POST) and `:batchDelete`.
  - Filter syntax is a string expression, URL-encoded, e.g.
    `weight.sample_time.physical_time >= "<iso>" AND weight.sample_time.physical_time < "<iso>"`.
  - The token comes from a normal OAuth2 code flow at `accounts.google.com/o/oauth2/v2/auth`, with
    their own "registration key"; `googlehealth_refresh_token` / `googlehealth_auth_token` /
    `googlehealth_token_end_time` live in SharedPreferences.
- **Scopes it asks for** (exact strings): `googlehealth.profile.readonly`,
  `googlehealth.activity_and_fitness.readonly`, `googlehealth.health_metrics_and_measurements.readonly`,
  `googlehealth.location.readonly`, `googlehealth.sleep.readonly`, plus `.writeonly` variants of sleep
  and the two data scopes when a Google destination is enabled.
- **File writers**: it bundles the Garmin **FIT SDK** (`com.garmin.fit.*`, `Sport`, `LapTrigger`,
  `SwimStroke`) and also builds TCX by hand with `XmlSerializer` —
  `TrainingCenterDatabase` in namespace `http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2`,
  with `ActivityExtension/v2`, `UserProfile/v2`, `ActivityGoals/v1`, `ProfileExtension/v1`.

**So the two halves come from different apps**: read side = Health Sync's Google Health v4 calls
above; write side = SyncMyTracks' `upload-service/upload`. Health Sync cannot be the write-side guide.

## The shortest path, built 2026-11-25

Google Health exports TCX itself, so no TCX/FIT writer is needed:

```
GET  https://health.googleapis.com/v4/users/me/dataTypes/exercise/dataPoints
       ?filter=exercise.interval.civil_start_time >= "<civil>" &pageSize=25
GET  https://health.googleapis.com/v4/users/me/dataTypes/exercise/dataPoints/{id}
       :exportExerciseTcx?alt=media        -> raw TCX bytes
POST https://connectapi.garmin.com/upload-service/upload/.tcx   (Bearer)
```

Notes that matter:
- `exportExerciseTcx` requires **both** `googlehealth.activity_and_fitness.readonly` and
  `googlehealth.location.readonly` in the token.
- `pageSize` for `exercise` is capped at 25, so paging is mandatory.
- Filter uses civil time (no `Z`), e.g. `2026-11-24T00:00:00`.
- Garmin's importer rejects TCX sports other than Running/Biking/Other; the app rewrites the rest
  to Running (`tcxSport` in `lib/garmin/garmin_upload.dart`).
- OAuth client needed: **Desktop app** type, package nothing to register, consent screen with the
  account as a test user. `google_sign_in` and its Android client are gone: an Android client never
  issues a refresh token, and Google blocks sign-in inside embedded WebViews
  (`disallowed_useragent`), so the only headless route is an installed-app client with PKCE
  against a loopback redirect opened in the real browser.

## Background sync, built 2026-11-25

```
GoogleOAuth.signIn()      browser -> accounts.google.com, PKCE S256
                          HttpServer on 127.0.0.1:8765 catches ?code=
                          exchange at oauth2.googleapis.com/token  -> refresh token
                          stored in flutter_secure_storage (key google_refresh_token)
GoogleOAuth.accessToken() refresh_token grant, cached 60 s short of expiry
WorkManager               12 h periodic, Constraints(networkType: connected),
                          runs after reboot, existingWorkPolicy: keep
backgroundDispatcher      same SyncService, output into SharedPreferences "background_log"
UI                        shows the last background log when the app opens
```

Rules that follow from this shape:
- The app has **one** auth flow for foreground and background; there is no silent re-auth.
- Test users get refresh tokens that **expire after 7 days**, so roughly weekly the app needs a tap
  on "Connect Google" again. Publishing/verification (or a Workspace "Internal" project) removes
  that; nothing else does.
- WorkManager is best effort: Android batches the 12 h period, so expect drift, not a timer.
- Client id/secret are injected at build time from `.env`:
  `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` (`GOOGLE_REDIRECT_PORT` is fixed at 8765 in the
  Taskfile). `.env` is gitignored.

## How Health Sync really gets the Google token (checked in the decompile)

`qv0.java:261-264` builds the consent URL by hand:

```
https://accounts.google.com/o/oauth2/v2/auth
  ?client_id=996604899558-qvvdq8em7qbst1abpgss1p53n2027etp.apps.googleusercontent.com
  &response_type=code
  &redirect_uri=https://server.healthsync.app/google-health-auth
  &include_granted_scopes=false
  &state=<their firebase token>
```

So: same authorization-code flow, but a **web** client whose secret lives on their server, and the
redirect lands on their broker (`server.healthsync.app`), which exchanges the code, keeps the
refresh token, and hands the app short-lived access tokens keyed by the firebase token. No client
secret is in the APK. The same broker pattern covers their other sources (`/withings`,
`/intervals-auth`, `/huawei`, `/garmin-refresh-token/`).

That is the one thing this app cannot copy without running a server: our redirect is
`http://127.0.0.1:8765/callback` and the refresh token stays in the phone's secure storage, which is
why the 7-day Testing expiry applies to us and not to them.
