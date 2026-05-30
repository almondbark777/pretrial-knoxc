# webapp — Knox County Pre-Trial FastAPI app

Serves 10 HTML pages + a JSON API against the Azure SQL database.
All routes are gated behind HTTP Basic Auth using a shared password and
a fixed 22-user allow-list.

## Pages

| URL | Live data | Source |
|---|---|---|
| `/` (dashboard) | ✓ | `defendants`, `check_ins`, `payments` |
| `/pretrial_app.html` (case management) | ✓ | full defendant bundle (3,309) |
| `/client_profile.html` (per-client lookup) | ✓ | `client_profiles_bundle()` |
| `/analytics.html` | ✓ | 6 live charts |
| `/violation.html` | ✓ defendant search (form still mockup) | `defendants` |
| `/intake.html` | — mockup | — |
| `/risk_tool.html` | — mockup calculator | — |
| `/screen.html` | — mockup randomizer | — |
| `/gps_alert_procedures.html` | — static reference | — |
| `/system_comparison_mockup.html` | — static explainer | — |

## Files

```
webapp/
├── app.py              FastAPI routes + Basic Auth middleware
├── queries.py          All SQL queries + connection pool + TTL cache
├── users.py            Allow-listed usernames (lowercase-matched)
├── requirements.txt    Deps
├── .env.example        Fill in DB_PASSWORD + APP_PASSWORD
├── templates/          HTML (4 patched with Jinja data injection)
└── static/             Reserved for assets
```

## Run

```bash
python -m venv .venv
source .venv/bin/activate   # Windows: .venv\Scripts\Activate.ps1
pip install -r requirements.txt
cp .env.example .env        # Windows: copy .env.example .env
# edit .env, paste DB_PASSWORD
uvicorn app:app --reload --port 8000
```

Open http://localhost:8000.  Browser prompts for credentials:

- **Username** — any email in `users.py`, case-insensitive
- **Password** — whatever you set `APP_PASSWORD` to (default `pretrialtestsite`)

## JSON API (same auth)

```
GET /api/stats        Dashboard KPIs
GET /api/defendants   Full case-mgmt bundle
GET /api/analytics    6 chart feeds
GET /api/officers     Officer caseloads
GET /api/activity     Recent events
GET /api/clients      Per-client lookup bundle
GET /api/whoami       { user: "alexander.bentley@knoxsheriff.org" }
GET /api/refresh      Clear the in-memory cache
GET /health           Auth-free; DB probe for uptime checks
```

## Auth notes

- `app.py` wraps the whole app in a `@app.middleware("http")` that
  requires Basic Auth everywhere except `/health`, `/static/*`, `/favicon.ico`.
- Usernames are case-insensitive.  Comparison uses `secrets.compare_digest`
  for the password.
- This is a **temporary** layer.  Real production should use Microsoft Entra
  Easy Auth on Azure App Service — flip the toggle in the portal, restrict
  to the Knox County tenant, and remove this middleware.

## Adding a new user

Edit `webapp/users.py`, add the email (any case) to `_RAW_USERS`, commit, push.

## Troubleshooting

- **"Database is not currently available"** on first load — Azure SQL
  serverless auto-paused.  Wait 15–30 s and retry.  `queries.py` retries
  on error 40613 automatically.
- **Browser keeps prompting** — wrong username or password.  Clear the
  saved credentials (Chrome: `chrome://settings/passwords`; Firefox:
  Settings → Privacy → Saved Logins) to reset the prompt.
- **"unhashable type: dict" in Jinja** — you're on starlette 1.0 and the
  old `TemplateResponse(name, {"request": request, ...})` signature.
  Use `TemplateResponse(request, name, {...})`.
