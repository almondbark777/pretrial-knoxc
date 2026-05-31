# Phase 7 — Admin & Data-Entry Layer

> Tracking doc (Brief Part 0.1 paper trail). Append-only. Design brief:
> [`PROMPT_admin_dataentry.md`](../PROMPT_admin_dataentry.md). Read
> `PHASE_4_GO_REWRITE.md` first for the compute/data-layer context this builds on.

---

## Entry — 2026-05-30 · Model: claude-opus-4-8 (1M context) · Effort: High (autonomous)

Built the **write / correction side** of the Go app — the website *outside* the
client-lookup tracker — server-rendered in the existing dark Wong-palette theme,
usable by non-technical supervisors with **no SQL**. The read-only pages
(dashboard, cases grid, analytics, calendar, profile, lookup) already existed
(Phase 4 G5); this adds delete/restore, field overrides, and per-defendant CRUD.

**Headline guarantee (PRIORITY 1):** a supervisor can find a wrongly-added person,
**Delete** them with one confirm, and they vanish from **every** view and **stay
gone across the next import** — recorded in the audit log, no SQL.

`go vet` clean · **all 26 tests pass** (`go test -count=1 ./...`) · Linux amd64
single binary builds (19.7 MB) · full live HTTP smoke test green.

---

## What was built

| File | Role |
|---|---|
| `db/migrations/003_admin_sqlite.sql` | New tables `deleted_idns` (tombstones) + `overrides`. Native SQLite, `IF NOT EXISTS`, mirrors 001 style. |
| `internal/db/admin.go` | `EnsureSchema` (startup bootstrap), tombstone/override **read** loaders, `DeletePerson`/`DeleteCase`/`RestorePerson`/`RestoreCase`, `SetOverride`/`ClearOverride`, `WriteAudit`, the override allow-list. |
| `internal/db/extension.go` | Per-defendant CRUD (notes / tags / court dates / reminders / violations) + `ListTombstones`/`ListOverrides`/`LoadExtras`. Every write audited in-tx. |
| `internal/db/db.go` | `BuildClients` now **filters tombstones** and **applies overrides** (loaded once per build). |
| `internal/compute/{helpers,compute}.go` | `NowET()` (ET audit timestamps); `Client.Overrides` field for UI flagging. |
| `internal/auth/auth.go` | **Supervisor tier**: `SUPERVISOR_EMAILS` subset of the allow-list, `IsSupervisor()`. |
| `internal/handlers/admin.go` | All write handlers + confirm/deleted/role-gating. |
| `internal/handlers/{handlers,service}.go` | Profile loads extras + role + overridable fields; dashboard/cases/analytics expose `IsSupervisor` (nav); cache cleared after every write. |
| `cmd/server/main.go` | `EnsureSchema` at startup; parse `SUPERVISOR_EMAILS` + `IMPORTER_RETIRED`; register the `/admin/*` routes. |
| `templates/` | New `message.html`, `delete_confirm.html`, `deleted.html`; `profile.html` extended (override panel + CRUD); supervisor "Deleted" nav link + flash on existing pages. |
| `static/app.css` | `.chip`, `.entry`, inline-delete button styling (reuses the existing palette/vars). |
| `internal/handlers/admin_test.go` | The propagation/audit/override/gating proofs (below). |

---

## The importer-proof delete — design

The ONE real constraint is the importer, not caution (testing phase; hard deletes
are fine — see Brief). `sharepoint_import.py` does a **full reload of
`raw_blue_book` every Sunday**, so a physical delete of a raw row is re-added on
the next import. The delete therefore works in **both worlds with no rework at
cutover**:

1. **Tombstone** → `deleted_idns(idn, case_number NULL, deleted_by, deleted_at, reason)`.
   `case_number NULL` = whole person; a case token = a single case.
2. **`BuildClients` filters tombstoned idns/cases** on every read (the set is
   loaded **once per build**). Because the tombstone lives in an extension table
   the importer never touches, the person stays gone across the Sunday reload.
   One filter → gone from **lookup, dashboard, cases grid, rosters, analytics,
   profile, calendar, and every `/api/*`** at once.
3. **App-owned extension rows** for that IDN are purged on a whole-person delete
   (notes/tags/court_dates/violations/reminders/overrides/pins/docs/scheduled).
   `audit_log` is deliberately **kept** — it is the recovery breadcrumb.
4. **`IMPORTER_RETIRED=true`** (config, default **false**) additionally does a
   **physical `DELETE`** of the `raw_*` rows. At that point the tombstone is a
   harmless no-op and delete is a plain row delete — **no code change** needed at
   SharePoint cutover.

This is the **only** path that ever writes to `raw_*` (Brief Part 5.4 respected).

`EnsureSchema` runs the 003 (and the 001 extension) DDL at startup
(`CREATE TABLE IF NOT EXISTS`), so the Go app self-provisions a fresh DB — no
manual migration step. `BuildClients` tolerates a pre-migration DB (the loaders
no-op when the tables are absent), so reads never depend on the write-side tables.

---

## Routes

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET  | `/admin/delete?idn=&case=` | supervisor | One-screen confirmation naming exactly who/what is affected |
| POST | `/admin/delete` | supervisor | Tombstone (+ purge extensions, + physical delete if retired) + audit |
| POST | `/admin/restore` | supervisor | Lift a tombstone (whole person or one case) + audit |
| GET  | `/admin/deleted` | supervisor | List of current tombstones (the breadcrumb) with Restore buttons |
| POST | `/admin/override` | supervisor | Upsert an `(idn, field)` override + audit |
| POST | `/admin/override/clear` | supervisor | Remove an override + audit |
| POST | `/admin/note/add` · `/admin/note/delete` | any officer | Notes CRUD |
| POST | `/admin/tag/add` · `/admin/tag/delete` | any officer | Tags CRUD |
| POST | `/admin/courtdate/add` · `/admin/courtdate/delete` | any officer | Court dates CRUD |
| POST | `/admin/reminder/add` · `/admin/reminder/delete` | any officer | Reminders CRUD |
| POST | `/admin/violation/add` · `/admin/violation/delete` | any officer | Violations CRUD |

All writes Post/Redirect/Get back with a flash `?msg=`. Destructive actions also
carry a vanilla `onsubmit="confirm(...)"` guard *and* work with `<noscript>`
(plain form posts). `/health` stays auth-free; nothing here touches the importer
or the timers.

## Role matrix

| Action | Officer (allow-list) | Supervisor (`SUPERVISOR_EMAILS`) |
|---|---|---|
| View everything | ✅ | ✅ |
| Add/edit/delete notes, tags, court dates, reminders, violations | ✅ | ✅ |
| Delete / restore a person or case | ❌ 403 | ✅ |
| Set / clear field overrides | ❌ 403 | ✅ |

Supervisors must also be on the 22-email allow-list (a `SUPERVISOR_EMAILS` entry
not on the allow-list is ignored). The current user's role is exposed to templates
so the Delete/override controls and the "Deleted" nav link **hide** when not
permitted.

---

## Field overrides (PRIORITY 2 — typo fixes to imported data)

`overrides(idn, field, value)`, applied in `BuildClients` **after** the raw read
by splicing the value into the row map — so all downstream values *and* the
compute layer see the corrected value. Restricted to a safe allow-list of
imported, per-person `raw_blue_book` columns: `pretrial_level`, `referral_date`,
`case_status`, `gps_type`, `closed_date`, `day_adjustment`, `supervising_officer`,
`defendant`. Overrides **do** feed money/compliance math (that's the point of
fixing a typo'd level) but are **never silent**: the profile flags overridden
values with an `override` badge and a banner, and lists them in a Corrections
panel with one-click clear. The correct long-term home remains SharePoint; this
is the immediate, audited stop-gap.

---

## Proof — suppression propagates everywhere (tests + live)

`internal/handlers/admin_test.go` runs against a temp copy of `db/kh222.db`
(the committed copy is never mutated; `EnsureSchema` applied):

- **`TestDeleteSuppressesEverywhere`** — picks a victim off the live missed-roster,
  asserts present in **BuildClients + grid + missed roster + lookup**, deletes,
  then asserts **absent from BuildClients + grid + missed roster + behind roster +
  lookup**, asserts it's still gone on a **second** `BuildClients` (import
  rebuild), then **restores** and asserts it's back everywhere. Audit rows for
  `delete_person` and `restore_person` confirmed.
- **`TestDeleteCaseKeepsPerson`** — deletes one case token of a multi-case IDN
  (chosen so the token covers only some rows); asserts the person survives with
  the expected row count, the token is gone from the case options, then restores.
- **`TestOverrideApplies`** — overrides an L2/L3 client's `pretrial_level` to `1`;
  asserts `BuildClients` reflects it, `Client.Overrides` flags it, and
  `ComputePTRFees` returns the **L1 flat $20 / 1 month** (compute saw the
  override); clear reverts. Audit rows for `override_set`/`override_clear`.
- **`TestSupervisorGating`** — drives the real `auth.Middleware`: a supervisor
  passes `requireSupervisor`, a non-supervisor gets **403**; an officer note-add
  succeeds and is audited.

```
go vet ./...        # clean
go test -count=1 ./...
  ok  internal/compute  (20 tests)
  ok  internal/db        (2 tests)   TestGoldenAgainstRealDB, TestMultiCaseRetained — unchanged, still green
  ok  internal/handlers  (4 tests)   the Phase-7 proofs above
```
(`TestDeleteCaseKeepsPerson` repeated `-count=20` with randomized map order — no
flakiness.)

### Live HTTP smoke test (binary vs a scratch copy of `db/kh222.db`)

Supervisor = `alexander.bentley@…`, officer = `Daniel.Harris@…` (identity via the
trusted `Cf-Access-Authenticated-User-Email` header).

- `/health` → `{"db":"up","ok":true}` (auth-free) ✅
- Nav: supervisor sees the **Deleted** link; officer does **not** ✅
- Officer `GET`/`POST /admin/delete` → **403**; supervisor `GET` → confirm page ✅
- Supervisor `POST /admin/delete idn=1704989` → 303 → `/api/clients?idn=1704989`
  now **404**, **0** occurrences in `/api/defendants`, and the row shows on
  `/admin/deleted` with its reason ✅
- `POST /admin/restore idn=1704989` → 303 → `/api/clients?idn=1704989` **200** again ✅
- **Per-case:** IDN 1365339 (cases `@1655893`, `@1655541`) — delete `@1655893` →
  profile still **200**, displayed case row switches to `@1655541`; restore → back ✅
- **Override:** HANCOCK 1704989 PTR `level 3 / owed 40 / 2 mo` → set
  `pretrial_level=1` → `level 1 / owed 20 / 1 mo`, profile flags `override`; clear
  → reverts to `level 3 / owed 40` ✅
- Officer override attempt → **403**; officer note-add → 303, note renders on the
  profile ✅

### Audit examples (real rows, ET-stamped)

```
2026-05-30 20:30:34 EDT | alexander.bentley@knoxsheriff.org | delete_person  | deleted_idns | 1704989 | smoke-test-wrong-entry
2026-05-30 20:30:34 EDT | alexander.bentley@knoxsheriff.org | restore_person | deleted_idns | 1704989 | tombstones removed: 1
```
Each write emits exactly one `audit_log` row: actor (Cf-Access email / session
user), ET timestamp (`compute.NowET`), action, target table, `row_id`=idn,
`col_name`=case token / overridden field, and old/new values where relevant.

---

## Configuration (env)

| Var | Default | Effect |
|---|---|---|
| `SUPERVISOR_EMAILS` | (none) | Comma/space-separated supervisor emails (∩ allow-list). Empty = no one can delete/override. |
| `IMPORTER_RETIRED` | `false` | `true` flips Delete from tombstone → physical `raw_*` row delete (SharePoint cutover). |

Set both in `webapp/.env` / the systemd unit alongside `APP_PASSWORD` /
`APP_SESSION_SECRET` / `SQLITE_DB_PATH`.

---

## Hard-rule compliance (Brief Part 5.4)

- ✅ **No `raw_*` writes** except the `IMPORTER_RETIRED` physical-delete path.
- ✅ Native SQLite only (`modernc.org/sqlite`); chi; gorilla/sessions. No new heavy deps.
- ✅ `/health` still auth-free; importer / timers untouched.
- ✅ Reuses `compute.CaseTokens` (`[,\s]+`), `compute.FmtOfficer`, ET timestamps.
- ✅ Same dark Wong-palette theme; icon+color, never color alone; confirm + audit on destructive actions; `<noscript>` fallbacks.

## Known limitations / caveats (documented, by design)

- **Overrides are keyed `(idn, field)`** per the brief, so an override applies to
  **all** of a multi-case IDN's rows. Fine for the "obvious typo" use case; a
  per-case override would need an `(idn, case, field)` key (future work).
- **Per-case delete suppresses any blue_book row whose warrant tokens include the
  deleted token.** For the common one-token-per-row data this is exact; for a
  grouped `@A @B` row, deleting `@A` removes that row (and its `@B`). Acceptable
  given the data shape; documented in `deleteRawByCase`/`caseSuppressed`.
- **Restore cannot recover physically-deleted rows** once `IMPORTER_RETIRED=true`
  (there's nothing to un-tombstone). Expected at cutover.

## Remaining (out of scope here)

- Roster-mode calendar (Brief 2.9 second mode) — still pending from Phase 4.
- Auth allow-list → config/DB (Brief 4.5) — supervisors are now env-driven, but
  the 22-email base list is still in `auth.go`.
- The work remains **untracked in git** (per the standing instruction — commit
  only when asked).

---

## Entry — 2026-05-30 (UX redesign + client-tracker landing) · Model: claude-opus-4-8 (1M)

Two follow-on changes after the admin layer landed, both per Alex's direction.

### 1. Ultra-modern UX (within the hard constraints)

Rewrote `static/app.css` into a cohesive modern **dark design system** — layered
surfaces, a refined type scale, sticky translucent top bar, elevated KPI cards
with hover lift + semantic accent bars, polished buttons (primary/ghost/danger),
modern focus-ringed inputs + custom selects, animated dismissible **toasts** for
the `?msg=` flash (replacing the plain banner), hover-highlight tables with sticky
headers, animated analytics bars, refined chips/badges, responsive + reduced-motion
guards. **No framework, no CDN, no SPA** — all server-rendered `html/template` +
vanilla CSS. The Wong palette is preserved for **status** (vermillion=risk,
sky=ok, amber=warn), always paired with an icon (✓ ⚠ ◯), never color alone.
KPI semantics tightened: "Behind on GPS" → vermillion, "Missed this month" → amber,
each with a ⚠. Templates touched: all of them (brand mark, toasts, nav). Verified
live in a browser (login, dashboard, profile, tracker shell) — clean and cohesive.

### 2. Client tracker stays the landing page (transition arrangement)

Alex wants the **existing client tracker to remain the front door** while the new
admin/data-entry app stays separate "until I'm happy with it," reachable by a
button (and a button back). Implemented without touching the tracker bundle (it
`document.write`s itself, so injection is unsafe):

- **`/` → `Home`** renders `templates/shell.html`: a slim dark top bar (brand +
  **"Admin & Data-Entry →"** button → `/dashboard`) framing the untouched
  `static/lookup/PTR_Client_Lookup.html` in an **iframe**.
- **`/api/lookup_data`** reimplemented in Go (`internal/db/lookup_data.go`) — the
  four datasets (bb/ci/pm/gp) remapped to the exact SharePoint headers the
  tracker's `colFind` expects, **with tombstones + overrides applied** so a
  deleted/corrected person propagates to the tracker too. Test
  `TestLookupDatasetsHonorsTombstoneAndOverride` proves it.
- **Dashboard moved `/` → `/dashboard`**; every new-app page gained a
  "← Client Tracker" nav link back to `/`.
- Verified live: `/` shows the tracker (2206 clients loaded, Behind-on-Coverage
  roster — fed by the Go endpoint) under the shell bar; the button round-trips to
  `/dashboard` and back. Iframe does not frame-bust.

This defers (does not cancel) the Part 0.2 retirement of the embedded tool — the
new app's own views still use the server-side math as the single source of truth.

### Tests after these changes

`go vet` clean · `gofmt` clean · **27 tests pass** (`go test -count=1 ./...`) —
the 26 from the admin layer + `TestLookupDatasetsHonorsTombstoneAndOverride`.
Linux amd64 single binary builds.

---

### Definition of done — met

A supervisor can, from the themed UI, find a wrongly-added person, **Delete** them
(one confirm) so they vanish from every view and **stay gone across the next
import**, recorded in the audit log — and officers can add/edit per-defendant
extension data. Flipping `IMPORTER_RETIRED=true` turns Delete into a physical row
delete with no other code change.

---

## Entry — 2026-05-30 (production hardening + follow-ons) · Model: claude-opus-4-8 (1M)

Follow-on work to make the write surface production-ready. All on `main`, tests green.

- **Roster-mode (team) calendar** (Brief 2.9): `/calendar.html` with no `idn`
  aggregates per-day counts (check-ins / payments / missed / due) across all clients
  (one open-preferred rep per IDN, no double-count) + month-total KPIs + month nav;
  `?idn=` keeps the per-client view. New "Calendar" nav link.
- **Auth allow-list → config** (Brief 4.5): `auth.New` reads `ALLOWED_EMAILS`
  (falls back to the built-in 22); covered by `internal/auth/auth_test.go`.
- **CSRF**: synchronizer-token on all `/admin/*` POSTs — `auth.CSRF` mints a
  per-session token, the form-rendering handlers embed it, and a `csrfGuard`
  middleware rejects mismatches (constant-time, fails closed). Verified live:
  no-token / wrong-token → 403, valid → 303, for both officer and supervisor writes.
- **Secure cookie**: `COOKIE_SECURE=true` marks the session cookie Secure (HTTPS
  behind Cloudflare). **Security headers**: `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: SAMEORIGIN` (clickjacking protection that still permits the
  landing page's own same-origin tracker iframe), `Referrer-Policy: same-origin`.
  No strict CSP — pages use small inline scripts; that's a separate decision.
- **Deploy prep**: `deploy/DEPLOY_GO.md` cutover checklist; documented
  `ALLOWED_EMAILS` / `SUPERVISOR_EMAILS` / `IMPORTER_RETIRED` / `COOKIE_SECURE` in
  `webapp/.env.example`; systemd unit notes the `static/lookup/` shipping requirement.
- **UX/a11y polish**: responsive wide-table horizontal scroll, single-row mobile
  nav, positive empty states, toast `role="status"`, `aria-label` on icon-only
  delete buttons.
- **Repo hygiene**: `.gitattributes` enforces LF (the deploy target is Linux),
  ending Windows CRLF churn.

Rate limiting was intentionally left to the Cloudflare edge (22 users behind
Access; an in-memory limiter adds risk for little gain).
