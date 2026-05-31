# Phase 0 — Codebase Inventory

> **Phase 0 tracking doc** (see WORKFLOW.md / Brief Part 0.1). Generated 2026-05-30.
> Read before Phase 2 (parity audit) or Phase 4 (Go rewrite). Phase numbers refer to WORKFLOW.md.

> ⚠ **Spec-version note:** the Brief names the canonical spec as `PTR Client Lookup.html` **v0.81** (CSV-based). The repo actually ships **v0.82 (SQL-connected)**, and the webapp *embeds* it — `queries_ext.lookup_datasets()` and `queries.client_profiles_bundle()` remap SQL rows back into SharePoint column headers so the embedded HTML app runs without CSV uploads. Phase 2 must therefore audit (a) the Python query layer **and** (b) the column-header remapping feeding the embedded HTML app. Confirm with Alex whether v0.82 supersedes v0.81 as the canonical spec.

---

## Asset extraction

`scripts/unpack.py` unpacks `PTR Client Lookup v0.82 (SQL-connected).html` into `assets/`.
Run: `python scripts/unpack.py "PTR Client Lookup v0.82 (SQL-connected).html" assets/`

The three editable JS assets (everything else is library code or fonts):

| UUID prefix | File | Size | Role |
|---|---|---|---|
| `8a6913e5` | `assets/8a6913e5-7c4a-4b1b-ad95-28756bd8bf2a.js` | 29,564 B | Data layer — parsing + business logic |
| `5037ee28` | `assets/5037ee28-cfe4-4239-9acd-a592fb130a29.js` | 67,143 B | React components |
| `ebec2ff2` | `assets/ebec2ff2-a156-49da-8ba5-909d4cbc0c08.js` | 17,553 B | App root — state + CSS injection |

---

## `webapp/app.py` — HTTP routing + auth middleware

| Method | Path | Handler / Queries called |
|---|---|---|
| middleware | all (except public paths) | cookie session → HTTP Basic auth gate |
| `GET` | `/login` | login page (public) |
| `POST` | `/api/login` | set session cookie |
| `POST` | `/api/logout` | clear session |
| `GET` | `/` | `queries.dashboard_stats`, `officer_caseloads`, `caseload_by_letter` → `index.html` |
| `GET` | `/pretrial_app.html` | `queries.case_management_bundle` → `pretrial_app.html` |
| `GET` | `/analytics.html` | `queries.analytics_bundle`, `dashboard_stats` → `analytics.html` |
| `GET` | `/client_profile.html` | `queries.client_profiles_bundle` → `client_profile.html` |
| `GET` | `/{page}.html` (9 static pages) | no DB; template render only |
| `GET` | `/api/stats` | `queries.dashboard_stats` |
| `GET` | `/api/defendants` | `queries.case_management_bundle` |
| `GET` | `/api/analytics` | `queries.analytics_bundle` |
| `GET` | `/api/officers` | `queries.officer_caseloads` |
| `GET` | `/api/activity` | `queries.recent_activity` |
| `GET` | `/api/clients` | `queries.client_profiles_bundle` |
| `GET` | `/api/refresh` | clears TTL cache dict |
| `GET` | `/api/lookup` | `queries.defendant_lookup` |
| `POST` | `/api/referrals` | `queries.insert_referral` |
| `POST` | `/api/check_ins` | `queries.insert_check_in` |
| `POST` | `/api/payments` | `queries.insert_payment` |
| `GET` | `/api/defendants/{idn}` | `queries.get_defendant_full` |
| `GET` | `/api/defendants/{idn}/details` | `queries.get_defendant_details` |
| `PATCH` | `/api/defendants/{idn}` | `queries.update_defendant` + `qx.write_audit` |
| `GET/POST/DELETE` | `/api/defendants/{idn}/notes`, `/api/notes/{id}` | `qx.list_notes`, `add_note`, `delete_note` |
| `GET/POST/DELETE` | `/api/defendants/{idn}/tags`, `/api/tags/{id}`, `/api/tags` | `qx.list_tags`, `add_tag`, `delete_tag`, `all_tag_labels` |
| `GET/POST/DELETE` | `/api/defendants/{idn}/court_dates`, `/api/court_dates/{id}`, `/api/court_dates` | `qx.list/add/delete_court_date`, `upcoming_court_dates` |
| `GET` | `/api/defendants/{idn}/audit` | `qx.audit_for_defendant` |
| `GET/POST` | `/api/violations` | `qx.list_violations`, `add_violation` |
| `GET` | `/api/pinned` | `qx.list_pinned` |
| `POST` | `/api/defendants/{idn}/pin` | `qx.toggle_pin` |
| `GET/POST` | `/api/prefs` | `qx.get_prefs`, `set_prefs` |
| `GET/POST` | `/api/reminders` | `qx.list_reminders`, `add_reminder` |
| `POST` | `/api/reminders/{id}/complete` | `qx.complete_reminder` |
| `DELETE` | `/api/reminders/{id}` | `qx.delete_reminder` |
| `GET` | `/api/alerts` | `qx.alerts_summary` |
| `GET` | `/api/overdue` | `qx.overdue_check_ins` |
| `GET` | `/api/my_day` | `qx.my_day_bundle` |
| `GET` | `/api/defendants/{idn}/timeline` | `qx.defendant_timeline` |
| `GET/POST/DELETE` | `/api/saved_searches`, `/api/saved_searches/{id}` | `qx.list/add/delete_saved_search` |
| `GET` | `/api/whoami` | returns session user from `request.state` |
| `GET` | `/health` | `queries.get_conn()` ping — **auth-free** |
| `GET` | `/dashboard`, `/case_management`, `/analytics` | redirects |

---

## `webapp/queries.py` — Core read/write queries

All queries are written in T-SQL dialect. SQLite mode uses a `sqlglot` translation shim via `sqlite_compat.py`. **The Go rewrite must replace all of this with native SQLite.**

| Function | Responsibility |
|---|---|
| `get_conn()` / `_connect()` | Connection singleton; retries on Azure serverless 40613; `DB_BACKEND=sqlite` mode |
| `cached(key, ttl, fn)` | 60-second TTL cache keyed by string constant; cleared by `GET /api/refresh` |
| `_fmt_officer(email)` | `Nicholas.Loveless@knoxsheriff.org` → `Nicholas Loveless` |
| `_fmt_date(v)` | Multi-format date → `MM/DD/YYYY`; tries 6 strptime formats + ISO-Z fallback |
| `_d(v)` | Decimal / str / None → float |
| `dashboard_stats()` | Month-to-date counts: new referrals, GPS installs, check-ins, fees collected |
| `officer_caseloads()` | Open caseload count per officer with percentage bar |
| `caseload_by_letter()` | Open-case histogram bucketed by first letter of defendant name |
| `recent_activity(limit)` | Blended feed of latest payments, check-ins, GPS events sorted by date |
| `analytics_bundle()` | 6 chart datasets: supervision levels, officer loads, check-in types, GPS types, payments by type, 12-month compliance trend |
| `case_management_bundle()` | Full active-roster bundle for `pretrial_app.html` — defendants + cases + check-ins + payments + GPS stitched into `{defendants, stats}` |
| `insert_referral(d)` | Create new defendant row + case rows; validates IDN uniqueness |
| `insert_check_in(d)` | Append a check-in event; validates defendant exists |
| `insert_payment(d)` | Append a payment event; validates amount > 0 |
| `get_defendant_full(idn)` | All editable defendant fields + joined case numbers |
| `get_defendant_details(idn)` | Slide-in drawer bundle: base defendant + top-25 check-ins + payments + GPS row |
| `update_defendant(idn, body)` | Partial update via column whitelist; optionally syncs `cases` table |
| `defendant_lookup(q, limit)` | Live search by name fragment or IDN prefix; used by data-entry pages |
| `defendants_for_dropdown()` | Minimal list for data-entry page dropdowns |
| `client_profiles_bundle()` | Full active-roster bundle for `client_profile.html`; includes `day_adj`, GPS install ISO date, column-hint objects for the HTML lookup app |

---

## `webapp/queries_ext.py` — Extension-table queries

| Function | Responsibility |
|---|---|
| `lookup_datasets()` | Returns `{bb, ci, pm, gp}` — raw tables remapped to SharePoint column headers so the embedded PTR Client Lookup HTML app can consume them without CSV uploads |
| `list_notes / add_note / delete_note` | Free-text notes on a defendant (`defendant_notes`) |
| `list_tags / add_tag / delete_tag / all_tag_labels` | Label tags on a defendant (`defendant_tags`); de-duped on (idn, label) |
| `list_court_dates / add_court_date / delete_court_date` | Per-defendant court date entries |
| `upcoming_court_dates(days)` | Court dates in the next N days; used by dashboard and My Day |
| `write_audit` | Best-effort write to `audit_log`; never raises |
| `audit_for_defendant` | Last N edits to a defendant row |
| `list_violations / add_violation` | Supervision violations (`violations`) |
| `list_pinned / is_pinned / toggle_pin` | Per-user pinned/starred defendants (`pinned_defendants`) |
| `get_prefs / set_prefs` | Per-user theme, default landing, JSON prefs blob (`user_preferences`) |
| `list_reminders / add_reminder / complete_reminder / delete_reminder` | Per-user/per-defendant reminders (`reminders`) |
| `overdue_check_ins(days)` | Open defendants with no check-in in last N days |
| `alerts_summary(officer)` | Alert badge counts: overdue check-ins, GPS pending DA, open violations, court this week, my reminders |
| `my_caseload / my_day_bundle` | Everything for the My Day page: caseload sorted by oldest check-in, alerts, reminders, upcoming courts, pins |
| `defendant_timeline(idn)` | Unified chronological history: check-ins, payments, GPS, notes, violations, court dates, audit edits |
| `list_saved_searches / add_saved_search / delete_saved_search` | Per-user saved filter combos (`saved_searches`) |

---

## `db/migrations/` — Schema

| File | Dialect | Responsibility |
|---|---|---|
| `001_app_extensions_sqlite.sql` | **SQLite** | Creates all 11 extension tables: `defendant_notes`, `defendant_tags`, `court_dates`, `audit_log`, `violations`, `saved_searches`, `pinned_defendants`, `user_preferences`, `reminders`, `defendant_documents`, `scheduled_check_ins`. `IF NOT EXISTS` guards; `AUTOINCREMENT` / `TEXT` types. |
| `001_app_extensions.sql` | **Azure SQL** | Same 11 tables with SQL Server types (`BIGINT IDENTITY`, `NVARCHAR`, `DATETIME2`, `BIT`). `IF NOT EXISTS (sys.tables)` guards. |
| `002_grant_app_reader_writes.sql` | **Azure SQL** | `GRANT INSERT/UPDATE/DELETE` to `app_reader` on 5 normalized tables + all 11 extension tables. `raw_*` tables intentionally excluded. |

---

## Key observation for Go rewrite

`queries.py` and `queries_ext.py` are written in **T-SQL** (`TOP`, `DATEFROMPARTS`, `TRY_CONVERT`, `ISNULL`, `IF EXISTS…UPDATE…ELSE INSERT`, `SYSUTCDATETIME`). The SQLite mode on `ptr1` works only via the `sqlglot` translation shim in `sqlite_compat.py`. The Go rewrite eliminates this entirely — all queries must be rewritten as native SQLite against `pretrial_release.db`.
