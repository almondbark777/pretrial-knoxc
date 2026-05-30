# pretrial-knoxc

Knox County Sheriff's Office — Pre-Trial Services web app.

A FastAPI site backed by an Azure SQL database, gated behind a shared-password
login for 22 allow-listed users. Replaces a SharePoint-list + Excel workflow
for officers tracking defendants, check-ins, payments, and GPS monitoring.

## Status

- ✅ **Database loaded** — 32K historical defendants + 3.3K active-roster with
  full case/check-in/payment/GPS history on Azure SQL Database.
- ✅ **App built** — 10 pages wired to live data (dashboard, case management,
  client profile lookup, analytics, violations, 5 mockup tools).
- ✅ **Auth** — HTTP Basic Auth middleware, 22-user allow-list, shared password.
- ⏳ **Hosting** — currently runs locally only. Next step: deploy to Azure
  App Service so demo users can reach it without Alex's laptop being on.
- ⏳ **Real SSO** — Microsoft Entra / @knoxsheriff.org accounts. Blocked on
  funding and IT involvement. Phase 3.

## Repo layout

```
pretrial-knoxc/
├── CLAUDE.md                        project memory for Claude Code
├── README.md                        (you are here)
├── .gitignore
├── .github/workflows/
│   └── azure-deploy.yml.example     rename to .yml when ready to auto-deploy
├── db/                              SQL schema + ETL scripts + built SQLite copy
│   ├── schema_azure_sql.sql
│   ├── load_azure.sql
│   ├── build_db.py, export_azure.py
│   ├── csv_clean/                   10 UTF-8 CSVs ready for BULK INSERT
│   ├── kh222.db                     SQLite mirror for offline reference
│   └── README.md                    schema + row counts + quirks
└── webapp/
    ├── app.py                       FastAPI routes + Basic Auth middleware
    ├── queries.py                   all SQL queries + pymssql pool + TTL cache
    ├── users.py                     22-user allow-list
    ├── requirements.txt
    ├── .env.example                 copy to .env, fill in secrets
    ├── templates/                   10 HTML pages (5 Jinja-patched)
    └── README.md                    app internals + API reference
```

## Running locally

Requires Python 3.10+ and git on your PATH.

```bash
cd webapp
python -m venv .venv
# Windows:      .venv\Scripts\Activate.ps1
# macOS/Linux:  source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env        # Windows: copy .env.example .env
# edit .env, paste your DB_PASSWORD, optionally change APP_PASSWORD
uvicorn app:app --reload --port 8000
```

Open http://localhost:8000 — browser prompts for:

- **Username:** any email in `webapp/users.py` (case-insensitive)
- **Password:** whatever `APP_PASSWORD` is set to (default `pretrialtestsite`)

First request after idle will take ~15 s — the serverless DB cold-starts.

## Making it live (without needing your laptop on)

Push to GitHub, deploy to **Azure App Service Free F1** ($0/mo), wire up
**GitHub Actions** for push-to-deploy. End state:

- Public URL: `<app-name>.azurewebsites.net`
- Users log in with their email + shared password from anywhere
- `git push` to main → ~2 min later the site is updated
- Works when your laptop is off, or you're on vacation

Open Claude Code in this folder and it will walk you through it — see
`CLAUDE.md` for the full project memory + deployment plan.

## Known quirks

- **pymssql.bulk_copy is broken on NVARCHAR** — stage into VARCHAR first,
  then server-side `INSERT SELECT`. See `CLAUDE.md` → "Quirks."
- **Azure SQL cold-start** — 15–30 s on first hit after auto-pause.
- **Mixed date formats** — source data has ISO, US, and Excel dates jumbled
  together. All date columns are `NVARCHAR(50)`. Use `TRY_CONVERT`.
- **`order` is a reserved word** — bracketed `[order]` in the schema.

## Security TODOs (before going wider)

- [ ] Rotate the Azure SQL admin password.
- [ ] Delete the wide-open `claude-sandbox` firewall rule in SQL Server → Networking.
- [ ] Create an `app_reader` DB user with only `db_datareader` role; switch the app to use it.
- [ ] Turn on Azure SQL auditing.
- [ ] HTTPS-only (App Service → TLS settings → HTTPS only → on).
- [ ] Swap shared-password auth for Microsoft Entra SSO when funding allows.

## License

Internal use — Knox County Sheriff's Office.
