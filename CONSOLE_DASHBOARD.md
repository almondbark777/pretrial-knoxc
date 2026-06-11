# Case Console (`/console`) — the app UI

> Append-only paper trail for the console (the phase-era trails were
> consolidated into `PROJECT_HISTORY.md` 2026-06-10; this doc continues live).
> Built 2026-05-31 → 2026-06-01 as "Direction B", chosen as THE app 2026-06-02;
> the classic interface was removed 2026-06-09. Brief:
> `PROMPT_new_dashboard_for_claude_code.md` (parent folder). Functional spec:
> `PTR_Dashboard_BUILD_SPEC.md`. Layout ref: `Pretrial-UI-Mockup.html`. Demo
> walkthrough: `CONSOLE_DEMO_RUNBOOK.md`. Long since merged to `main`.

## What it is

A **second, commercial-inspired dashboard** that runs **alongside** the existing
`/dashboard` (not replacing it), so the two UX directions can be demoed side by side —
the same coexistence pattern as the tracker/`/dashboard` split (Brief Part 0.2).

The client tracker at `/` now shows **two top-bar buttons**:

- **Admin & Data-Entry →** (`/dashboard`) — Direction A, the existing dark top-nav app (untouched).
- **Case Console →** (`/console`) — Direction B, this app.

**Visual contrast is the point.** `/dashboard` is a dark, top-nav app; `/console` is a
**light professional enterprise SaaS** shell (navy left sidebar, white panels, dense
tables, status chips) with a dark-mode toggle. Wong colorblind-safe palette for status
(vermillion=risk, sky=ok, amber=warn), always paired with an icon + text label.

## Hard rule honored: one source of truth for the math

The console is a **pure presentation layer**. Every number comes from the existing
server-side math — `computeStats`, `behindRoster`, `missedCheckInsRoster`, `defendantRows`,
`ComputeCheckIns` / `ComputePTRFees` / `ComputeGPS`, `GetEventsForClient`. **No business
rule is reimplemented.** Proven by tests + live:

- new `GET /api/clients/{idn}` is field-for-field identical to the old `/api/clients?idn=`,
- console "Behind on GPS" count == `/api/stats` behindGps == the tracker's "Behind on Coverage",
- Reports rates are derived from the same totals (e.g. check-in compliance = (Open−Missed)/Open).

## Screens (Build-Spec milestones 1–2, plus extras)

| Route | Screen |
|---|---|
| `GET /console` | Dashboard "My Caseload" — 5 KPI cards (deep-link to filtered views) + alert feed + today's schedule + All/My-caseload scope toggle |
| `GET /console/clients` | Roster table — live filter + sort, URL-param filters (shareable), 50/page pagination, **bulk select** (checkbox column + select-all-page / select-all-filtered → Export-selected CSV + bulk-action bar), zero-match **empty-state** ("Clear all filters"), hover "✓ Check-in" quick-action, Level/Status/Compliance chips, avatars |
| `GET /console/clients/new` | **Intake wizard** — 6 steps (Identity → Case → Level → Conditions → Agreement → Assign), per-step validation, save-draft, demo-safe submit |
| `GET /console/clients/{idn}` | Client record — sticky header + 7 tabs (Case Summary / Conditions / Check-ins / Court / Payments-Fees / Documents / Activity) + reverse-chron timeline + missing-critical-info banner |
| `GET /console/calendar` | Calendar — roster (team) mode + per-client mode (reached from a record), month nav, print |
| `GET /console/compliance` | Behind-on-GPS (owed/paid/behind columns + waiver flag) + Missed-Check-Ins rosters, CSV export, print, "matches tracker" note |
| `GET /console/reports` | Population/level/vendor/officer bars + compliance & fee-collection rate cards + fees outstanding |
| `GET /console/admin` | Roles, conditions library, reminder templates (placeholders) + real audit trail + tombstones |

Track-date "**as-of**" control in the top bar (cookie `ptc_asof`, helper `trackFrom`)
re-runs the whole console's compute against any date — same `trackDate` contract as the
tracker (Build-Spec §3.3/§8.2), defaults to today EST. Keyboard: `/` search, `g`+letter
nav, `n` new note, `Esc` close, **`?` opens a keyboard-shortcut help overlay** (suppressed
while a text field is focused, so typing "?" in search never triggers it).

## Writes — demo-safe, and several are real

Write actions on the record open polished modals. **Note / Check-in / Court date /
Violation** persist for real via the existing **CSRF-guarded `/admin/*` endpoints** (which
write extension tables + `audit_log` only — never `raw_*`), then redirect back to
`/console/clients/{idn}` with a flash toast and the new item shows in the Activity timeline.
**Send Reminder is log-only** (Build-Spec §5.6): it records a reminder row marked
`[channel, logged not sent]` — no provider wired. **Court-outcome logging** (§5.6,
the FTA-tracking step) is live: after a hearing, "Log outcome" records the result +
next date on the `court_dates` row (additive `outcome`/`next_date` columns via a
tolerant `addColumnIfMissing` migration in `EnsureSchema`), shown as a ✓ chip. The **intake wizard's final step creates a
real native record** via `/admin/add_defendant` (Phase-10 path, writes `added_defendants`,
merged into every view by IDN) and lands on the new record — verified live. The shared
`safeNext` helper lets these endpoints redirect back to `/console/*` (open-redirect-guarded).
Tag / Pin / Override / Waive remain demo-safe "coming soon" stubs this pass
*(all four have since shipped — Tag and Override were already real by 2026-06-09,
Pin 2026-06-09, Waive 2026-06-10; only Upload Document is still a stub)*; the clients
table also has a hover **"✓ Check-in"** quick-action that persists. The roster's
**bulk-select** bar (appears on selection) offers **Export selected** (real — client-side
CSV of the chosen rows) and **Check-in selected** (real — POSTs to `/admin/checkin/bulk`,
which logs one audited check-in per selected client and returns a real count toast).
Selection is keyed by IDN so it survives paging and sort, and is cleared when a
filter changes (so the count never refers to hidden rows).

All real write forms (those POSTing to `/admin/*`) carry a **double-submit guard**: once a
form starts submitting (after HTML5 validation passes), its submit button disables and shows
"Saving…", so a double-click during a demo can't fire a duplicate POST. The guard adds no
`preventDefault` and disables on a later tick, so the submission itself is unaffected.

## Files

- `internal/handlers/console.go` — page handlers + `trackFrom` + `consoleBase` + `/api/clients/{idn}` + `distinctOfficers` + `nextCourtByIDN`
- `internal/handlers/console_view.go` — view-models (Chip primitive, dashboard, roster rows, full record), `pct`, formatting helpers
- `internal/handlers/console_test.go` — 9 tests (parity vs roster fns, chips, initials, pct, distinctOfficers, profileBack open-redirect, activity sort)
- `internal/db/extension.go` — `ListAllCourtDates` / `ListAllViolations` (tolerant of missing tables)
- `internal/handlers/admin.go` — `profileBack` honors same-origin `next`
- `internal/handlers/service.go` + `internal/models/models.go` — `RosterRow` gains Owed/Paid/Waived (populated in `behindRoster`)
- `cmd/server/main.go` — routes + template funcs (money/moneyi/moneyP/intP/boolP/initials/lower/evclass) + `moneyFmt`
- `static/console.css` — light+dark design system, Wong palette, print
- `templates/console_partials.html` — `c_top`/`c_bot` chrome, sidebar, top bar, search, theme/as-of/keyboard JS, `chip`/`lvlchip`, demo-modal + toast helpers, `?` shortcut-help overlay, a11y (aria-current, keyboard rows)
- `templates/console_{dashboard,clients,record,intake,calendar,compliance,reports,admin}.html`
- `templates/shell.html` — second tracker button

## Verification

`go build ./cmd/server` ✓ · `go vet ./...` ✓ · `gofmt` clean ✓ · full `go test ./...` green
(9 new console tests + all existing) ✓ · `/health` → `{"ok":true,"db":"up"}` ✓. Existing
tracker + `/dashboard` unchanged. Live browser smoke (preview, port 8099): login → every
page → as-of time-travel → intake wizard full flow → real note/check-in/reminder writes
persist and show in Activity → dark mode → mobile → filter deep-links → no console errors.

## Promotion to primary app + fixes (2026-06-02)

Per Alex, the Case Console is now the **main app going forward** (client tracker stays the
landing screen at `/`; the old `/dashboard` is demoted to a small "Classic view" link, kept
reachable, not deleted). Tracker top bar: solid **Open Console →** + low-key **Classic view**;
console sidebar/user-menu lost the "Direction B · runs alongside /dashboard" framing.

Two correctness fixes shipped the same session (tests + live-verified):

1. **Roster date columns sort chronologically.** The clients table's *Next Court* and
   *Next Check-in* columns sorted as text on the display strings (`"Jan 2"`), so clicking the
   header ordered dates alphabetically by month (Apr, Aug, Dec…). Added ISO sort keys
   (`NextCourtSort`/`NextCheckInSort` = `2006-01-02`, blank → `9999-12-31` sentinel so empties
   sort last) on `ConsoleClientRow`; `nextCourtByIDN` now returns a `courtCell{Display,Sort}`;
   template `data-sort` points at the ISO keys. Live: sorting Next Check-in now yields
   Oct 14 → Dec 19 → Feb 27 → … → Jun 5 (overdue first, chronological). Test
   `TestConsoleClientRowsDateSortKeys`.

2. **CSV exports honor the as-of date.** `ExportBehind/ExportMissed/ExportCases` hardcoded
   `compute.TodayET()`, so a file downloaded from the compliance page's "historical view"
   silently contained *today's* roster (and a today filename). They now use `s.trackFrom(r)`
   (the `ptc_asof` cookie) and stamp the filename with that date. Legacy `/dashboard` has no
   as-of control → no cookie → unchanged (still today). Live: as-of 2026-01-15 →
   `behind-gps-2026-01-15.csv` / 11 rows vs today `behind-gps-2026-06-02.csv` / 134 rows.
   Test `TestExportHonorsAsOf`. (`stamp()` kept — still used by the today-anchored EM-fees export.)

3. **"My caseload" now shows the officer's own court dates + violations.** The dashboard's
   scope toggle hides schedule/alert rows with `data-mine="false"` (`body.scope-mine` CSS), but
   court schedule items were hardcoded `Mine:false` and violation alerts never set `Mine` at
   all — so an officer's *own* clients' court appearances and violations vanished under "My
   caseload." Both now attribute to the client's supervising officer via new
   `officerForIDN(clients, idn)` + the existing `mine()` closure. Also tightened "Court Dates
   This Week" from an 8-day window (`track+7`, inclusive of today) to a rolling 7-day window
   (`track+6`). Tests `TestConsoleDashboardCourtMine`, `TestConsoleDashboardViolationMine`.

## Performance hardening — roster windowing (2026-06-02)

Direction from Alex: "super modern UI that the most basic little office computers can handle"
— **performance first**, target modern Edge/Chrome on weak hardware. The clients roster was the
bottleneck: it server-rendered **every** row (~2,157 in the offline DB; ~3,300 live) as full
`<tr>` markup with chips, then filtered/sorted/paged client-side via `display:none`. So the
whole roster lived in the DOM.

**Fix: true windowing.** The roster now ships once as a compact short-keyed JSON array
(`rosterRowsJSON` → `template.JS`, embedded in `<script>`; Go's json.Marshal escapes `<` so it's
safe in a script tag). The template (`console_clients.html`) holds an empty `<tbody>`; JS filters
/ sorts / pages the in-memory array and renders **only the visible 50-row page** into the DOM,
with one set of delegated `tbody` listeners (not 50×N). Chips are rebuilt in JS from `l`/`st`/`cm`
to match the `{{template "chip"}}` markup. All prior features preserved: live filters, URL-param
seeding + KPI deep-links, column sort (date columns use the ISO `ncs`/`cis` keys), bulk select /
select-all-filtered / export-selected / demo bulk check-in, per-page select-all, CSV export (now
built from the data, not DOM scraping), quick check-in modal, keyboard row nav, empty state.

**Measured (offline DB, /console/clients, 2,157 rows):**
- HTML payload **2,366 KB → 660 KB** (−72%)
- DOM nodes **43,347 → 1,201** (−97%)
- Always ~50 rows in the DOM regardless of caseload size (live ~3,300-row set would have been
  ~66k nodes / ~3.6 MB before).

Verified live: filter (smith→27), compliance=behind→134, chronological check-in sort
(Oct→Dec→Feb→…→Jun), paging (51–100 of 2157), select-all (50 selected + "select all filtered"),
quick check-in modal (correct IDN/name), empty state + clear, no console errors. Test
`TestRosterRowsJSON` pins the JSON contract + `<script>`-injection safety.

## Go-live stats epoch — Jun 1, 2026 (2026-06-02)

Alex: "I only want to show overall stats start from 01jun26 … label everything so there isn't any
confusion." Decision (confirmed): **restrict event tallies to on/after the epoch + label; keep
full time-travel; per-client records keep full history.**

- **Epoch constant:** `compute.StatsEpoch()` = 2026-06-01 (noon-UTC) + `compute.StatsEpochLabel` =
  "Jun 1, 2026". Single source. (Hardcoded go-live; could move to env later.)
- **Restricted (genuine lifetime event tally):** the dashboard **Open Violations** KPI + the
  violation **alert feed** now count only violations dated ≥ epoch (`violationsSinceEpoch` in
  service.go; undated/unparseable dropped). Applied in the `Console` handler before
  `consoleDashboard`. Per-client record/timeline still uses the unfiltered list (full history).
- **Left as current/lifetime (NOT date-scoped, by design):** roster-size counts (Active, GPS
  active, Behind-on-GPS, Missed-this-month) are *current state*; Reports fee owed/paid are
  *lifetime balances per client* (real outstanding money doesn't reset at go-live). These are now
  **labeled** as such rather than recomputed.
- **Labels (so the period is unambiguous):** `Epoch` added to `consoleBase` → every page. Dashboard:
  "since Jun 1, 2026" under the Open Violations card + an epoch note ("Roster counts are current …
  Activity tallies (violations) count from go-live · Jun 1, 2026"). Reports: "· go-live Jun 1,
  2026" in the sub + fee panel note clarifies balances are current/lifetime. Compliance: note that
  both rosters are current standing (system go-live · Jun 1, 2026).
- Test `TestViolationsSinceEpoch`. Verified live: labels render on dashboard/reports/compliance, no
  console errors. **Open question for Alex:** if you want a *"payments collected since go-live"*
  aggregate (distinct from the lifetime owed/paid balances), that's an additive metric I can build
  — say the word.

## Officer feedback (2026-06-02)

Three asks from an officer using the system:

1. **Per-check-in notes (DONE).** Stoney wants to document GPS fitments etc. on a check-in. Added a
   `note` column to `added_check_ins` (CREATE + `addColumnIfMissing` migration), threaded through
   `db.AddCheckIn(... note ...)`, `ListAddedCheckIns`, `models.AddedCheckIn.Note`, the
   `/admin/checkin/add` handler (`note` form field). Both console check-in modals (record +
   roster quick-action) now have a "Notes (fitment details, etc.)" textarea; the record's Check-ins
   tab shows a "Logged check-ins" panel (type · date · note · author, newest first). Test
   `TestAddPaymentAndCheckInFlowIn` asserts the note round-trips. Verified live: logged
   "In-person · GPS fitment — strap sized, base unit #4417 issued" → renders on the record.

2. **Import cadence (INFRA — not a code change here).** A GPS defendant added to Blue Book on the
   1st and released same-day had no tracker info — the Sunday-only/weekly reload is too slow. Needs
   the ptr1 import pipeline (Power Automate export + `ptr-import.timer`) bumped to **daily, ideally
   multiple times/day**. App-side data-entry ("+ Add client") is a stopgap for same-day intakes.
   ACTION: raise import frequency on ptr1; confirm Power Automate export cadence.

3. **Phone vs In-Person compliance — DONE ON BOTH SITES (2026-06-02).** Decision (Alex): clients
   must do **both** an in-person AND a phone check-in at their level's cadence (phone cadence = same
   as in-person), and **fix both sites**. Implemented in the canonical `compute.ComputeCheckIns`:
   each window now tracks `SatisfiedInPerson` / `SatisfiedPhone`; `Satisfied = both`. Added
   `compute.CheckInKind(type)` (in-person = "In Person"/"In-person"/office; phone = phone/virtual/
   video/tele; junk → neither) and `LastInPerson` / `LastPhone` to `CheckInResult`.
   `missedCheckInsRoster` now requires both types in the month (detail says which is missing). Record
   UI: Case Summary shows **Last In-Person / Last Phone**; Conditions tab split into "… in-person
   check-in" + "… phone check-in", each Current/Behind. Golden tests updated (REASONOVER 3→4 missed;
   in-person-only no longer satisfies). Full `go test ./...` green. **Verified live on the officer's
   exact example — LITTLEJOHN, IVAN WADE (20902):** last in-person Apr 1 vs last phone Apr 28; his
   phone-only weeks (Apr 6/13/20) now correctly **missed** (were "satisfied" before); record shows
   both ⚠ Behind. **Virtual** treated as a remote (phone-bucket) contact. **REMAINING: update the
   bundled canonical tracker HTML (iframe at /) to the same rule via the ptr-client-lookup skill, so
   both sites agree before the push** (Go and tracker now intentionally diverge until that lands —
   the old md5-parity with v0.82 is deliberately broken; `tools/parity_ref.py` also needs the rule).
   **TRACKER BUNDLE DONE (v0.83):** via the ptr-client-lookup skill (unpack → edit → validate →
   repack), applied the identical rule to the canonical JS in `static/lookup/PTR_Client_Lookup.html`:
   added `_ciKind()` (mirrors Go `CheckInKind` exactly — classifiers now byte-identical: in-person =
   inperson/office/walkin, phone = phone/text/call/virtual/video/tele, else neither), `computeCheckIns`
   windows now require both types (+ `satisfiedInPerson`/`satisfiedPhone` + `lastInPerson`/`lastPhone`),
   and the `MissedCheckInsRoster` component requires both types in the month. Babel-validated, repacked,
   version bumped to v0.83. **Verified live in the repacked bundle running in the browser: Littlejohn
   20902 → lastInPerson 2026-04-01 / lastPhone 2026-04-28, phone-only weeks Apr 6/13/20 = missed —
   byte-identical to the Go API result. No console errors.** Go ↔ tracker now agree. Backup of the
   pre-edit HTML at `_tracker_work/PTR_Client_Lookup.BEFORE.html` (gitignored). ~~Still TODO (dev-only,
   non-gating): update `tools/parity_ref.py` to the both-types rule~~ **DONE 2026-06-09** — `_ci_kind()`
   added (mirrors `CheckInKind`), all three window loops require both types, output gains
   `satisfiedInPerson`/`satisfiedPhone`/`lastInPerson`/`lastPhone`; re-verified on Littlejohn 20902
   (lastInPerson 2026-04-01 / lastPhone 2026-04-28, Apr 6/13/20 phone-only weeks missed — matches Go).

## Parked (revisit)

- **"Open Violations" KPI deep-link** (`console_dashboard.html`) points at `/console/compliance`,
  which only shows the Behind/Missed rosters — there is no violations roster, so the target is
  misleading. Either build a small violations roster (filter the alert feed / a dedicated page)
  or repoint the card. Parked 2026-06-02 at Alex's request ("note it and we'll come back").
  **RESOLVED 2026-06-09** — the compliance page gained a Violations roster (epoch-scoped,
  CSV export, rows resolve to the client record) in the console-only commit; the KPI lands
  on real rows now.

## Performance — server-side compute + compression (2026-06-02)

Follow-on to the roster windowing. The `BuildClients` data is cached 60s, but the compute layer
ran per request — with redundancy:

1. **Dashboard computed the two heavy rosters twice.** `consoleDashboard` called `computeStats`
   (which runs `behindRoster` + `missedCheckInsRoster`) *and then* ran both rosters again for the
   alert feed. Extracted `rosterStateCounts` (the cheap state tallies, no per-client compute) so
   `computeStats` keeps its output; `consoleDashboard` now computes each roster **once** and reuses
   it for both the KPIs and the alerts. (`computeStats` unchanged for its other callers.)
2. **Clients roster did wasted per-client compute.** `consoleClientRows` went through
   `defendantRows`, which computes `ComputeGPS` + `ComputePTRFees` + `ComputeCheckIns` per client —
   but the roster only needs Behind/Missed flags + next check-in, and then computed
   `ComputeCheckIns` *again*. Now it builds rows directly: behind/missed once, **one**
   `ComputeCheckIns` per client, **no** GPS/PTR compute. Behavior-identical (verified: 2,157 rows,
   behind=134, same first row/chips). `defendantRows` stays as-is for `/api/defendants` + exports.
3. **Gzip compression** (`chi middleware.Compress(5)`) added to the stack. Responses now compress
   when the client accepts it. Verified: `/metrics` → `Content-Encoding: gzip`; `console.css`
   29,354 → 7,111 bytes (−76%); the ~660 KB roster JSON compresses ~85–90% on the wire — the
   biggest win for slow office LANs. (Cloudflare may also compress at the edge; this guarantees it
   regardless of path and shrinks app→tunnel bytes too.)

All verified live (rows render, no console errors), full `go test ./...` green, vet/gofmt clean,
`TestConsoleDashboardParity` still holds.

## Perf — next candidates

- **Compliance page** server-renders both rosters in full (offline: 1,538 rows / ~14.6k nodes,
  521 KB — but the 1,399 "missed this month" is inflated by the stale fixture; live "missed
  **this** month" is much smaller). It's a scan/print page (Ctrl+F + full-list print + CSV), so
  windowing would hurt that workflow. If the live missed roster proves large, prefer modern-only
  `content-visibility:auto` + `contain-intrinsic-size` (keeps DOM intact for find/print) over
  paging. Left as-is for now.

## Follow-ups (if Direction B is chosen)

- ~~True virtualization for the roster~~ **DONE 2026-06-02** — windowing: only the visible 50-row page is in the DOM; full roster held as compact JSON. See "Performance hardening" above.
- Capture the full intake (score, statute, conditions, signed agreement) — only the core fields persist today; the rest are collected but not yet stored.
- ~~Pin/unpin clients (no endpoint yet)~~ **DONE 2026-06-09** — `/admin/pin/toggle`, record ⋯ toggle + badge, dashboard Pinned Clients strip. *(Bulk select, Export-selected, AND the bulk check-in write path (`/admin/checkin/bulk`) are all built.)*
- Add a `channel` column to `reminders` (currently folded into the body) when a real SMS/email provider is wired.
- Promote roles/conditions/templates from placeholders to real config tables.

## Entry — 2026-06-09 — Classic interface REMOVED; drug screens on the record

Alex: **"GET RID OF THE CLASSIC INTERFACE."** The Direction-A dark `/dashboard` app
(demoted to a "Classic view" link on 2026-06-02) is gone. The console is now the
only app UI; the bundled client tracker stays the landing page at `/`.

**Removed** (handlers + templates deleted): `/dashboard` (index.html), `/my_day.html`,
`/pretrial_app.html`, `/analytics.html`, `/calendar.html`, `/client_profile.html`
(profile.html), `GET /admin/add_defendant` (add_defendant.html), the `myDay` helper +
`models.MyDay` + its test. **Every old URL 302s to its console equivalent** —
`/client_profile.html?idn=X` → `/console/clients/X`, `/calendar.html?idn&month` →
`/console/calendar` with the query carried over, add-form → the intake wizard
(pinned in `cmd/server/main_test.go` TestLegacyRedirects).

**Kept** (not "the classic interface"): the printable `/reports/*` pages, the
supervisor utilities `/admin/{delete,deleted,audit}` (re-chromed — `sitenav` now
links Tracker/Console/Reports/Deleted/Audit only), all `/api/*` endpoints (the
console's global search uses `/api/lookup`), CSV exports, and all `/admin/*` POSTs.

**Console parity added for what only classic had:**
1. **Drug Screens on the record** (the feature shipped earlier today existed only on
   the classic profile): new "Drug Screens" tab (toned result chips, delete, violation
   hint), "Record Drug Screen" modal POSTing to the existing `/admin/drugscreen/add`,
   a "Last Drug Screen" Case-Summary field (risk-toned when positive/refused), screens
   merged into the Activity timeline, and a "Record drug screen" overflow-menu item.
   `ConsoleDrugScreen` VM + `drugScreenChip` in console_view.go; tests
   `TestDrugScreenChip` + `TestConsoleRecordDrugScreens`.
2. **Supervisor actions on the record's ⋯ menu**: "Audit history" (`/admin/audit?idn=`)
   and "Delete client…" (`/admin/delete?idn=` confirm flow — was only reachable from
   the classic profile).
3. **Cross-links**: console Admin → "Full audit log →" + "Manage / restore →";
   console Reports → "Printable reports →" (`/reports`).

**Redirect-default fixes**: `profileBack`/`safeNext` defaults, the override handlers
(which previously hardcoded the classic profile and ignored the console's `next` —
latent bug), `AddDefendant`, and `requireSupervisor`'s message-page Back all now
default to `/console/...`.

Verified live in the preview: all 7 legacy URLs redirect (with query carry-over),
tracker top bar has no Classic pill, every console page 200s with zero classic
references and no JS errors, drug screen add → renders in tab/summary/activity →
delete works (flash toasts both ways), supervisor pages render under the new chrome.
`go build`/`go vet`/`gofmt` clean; full `go test ./...` green.

## Entry — 2026-06-09 (later) — Undo last delete, real Pin client, parity_ref rule

Alex: "lets keep improving things." Three open items off the notes:

1. **One-click "Undo last delete" — rebuilt for the console** (the old
   `feature/undo-last-delete` branch had built it into the now-deleted classic
   dashboard; that branch + its `ptr-wt-undo` worktree are fully superseded and
   can be discarded). New `handlers.UndoLastDelete` (`POST
   /admin/undo_last_delete`, supervisor-gated, CSRF, reuses
   RestoreCase/RestorePerson which audit) restores the newest tombstone
   (`ListTombstones` is newest-first) and flashes "Restored NAME (IDN …)".
   Console Admin's tombstone panel header now carries the ↩ button (named
   confirm) next to "Manage / restore →". Graceful "Nothing to undo" when the
   table is empty. Test `TestUndoLastDelete` (blocked non-supervisor / restore /
   empty). Closes the STATUS nice-to-have.

2. **Pin client is real** (was an `openAction('soon')` stub). The
   `pinned_defendants` table from migration 001 (per-user, `UNIQUE(user_id,
   idn)`) is now mirrored into `ensureSchemaSQL` and actually used:
   `db.TogglePin/IsPinned/PinnedIDNs` (pins.go — audited `pin_add`/`pin_remove`,
   tolerant of pre-001 snapshots), `POST /admin/pin/toggle`
   (handlers/pins.go), record ⋯ menu toggles Pin/Unpin + a "📌 Pinned" badge,
   and the dashboard shows a **Pinned Clients** quick-list strip (pincard chips →
   records) between the KPIs and the feeds. Pins are per-officer; purged on
   whole-person delete (already in extensionTablesByIDN — note a delete→undo
   round-trip therefore drops the pin, same as notes/tags). Tests:
   `TestPinToggle`, `TestPinsPurgedOnPersonDelete`, `TestPinnedRows`.

3. **`tools/parity_ref.py` updated to the v0.83 both-types check-in rule**
   (closed the dev-only TODO above — see strikethrough).

Verified live in the preview: pin → flash + badge + dashboard strip → unpin →
strip gone; record ⋯ → Delete client → confirm page → delete → `/admin/deleted`
→ console Admin → ↩ Undo → "Restored ROBERTSON, CHARLES (IDN 148445)" →
record back. No JS errors. `go build`/`vet`/`gofmt` clean, full `go test ./...`
green. (Preview screenshots of `/console` time out on the stale fixture's huge
alert feed — rasterization only; the page itself responds fine.)

## Entry — 2026-06-09 (later still) — Saved views, render-on-demand rosters

Continuing "keep improving":

1. **Saved views on the roster** — the `saved_searches` table from migration 001
   (unused since the Python days) now backs per-officer named filter combos.
   "★ Save view" in the filter bar captures the current controls into a named,
   sanitized query (`sanitizeViewQuery` keeps only q/status/level/officer/comp/gps
   — injected params drop); chips under the filter bar re-apply a view in one
   click (they're plain URLs, so they compose with the existing URL-param
   seeding); × deletes (owner-scoped — `DeleteSavedView` matches user_id, so a
   guessed id does nothing). Upsert by name. Audited `view_save`/`view_delete`.
   Table mirrored into `ensureSchemaSQL`. Files: `internal/db/savedviews.go`,
   `internal/handlers/savedviews.go`, `models.SavedView`, `viewChips` VM
   (template.URL safe — query re-encoded server-side at save time), routes
   `POST /admin/view/{save,delete}`. Tests: `TestSavedViews` (lifecycle +
   foreign-delete blocked + audit), `TestSanitizeViewQuery`.

2. **Render-on-demand for the long scan/print lists** — implemented the
   approach the perf notes prescribed: `.cv-auto{content-visibility:auto;
   contain-intrinsic-size:auto 900px}` on the three compliance roster
   containers and the record's Check-ins/Activity timelines. Off-screen rosters
   skip layout+paint on weak office PCs; the DOM stays intact so find-in-page
   still matches and print renders in full (plus a `@media print` visible
   fallback). Verified live: computed `content-visibility:auto` on all three
   compliance `.tscroll`s (713 fixture rows).

3. Stale doc fixes: bulk check-in write path has been real for a while
   (`/admin/checkin/bulk`) — the "demo-safe stub" notes above are corrected.

All live-verified in the preview (save → chip → re-apply 140/2158 filtered →
delete; compliance + record render clean, no JS errors). Full `go test ./...`
green, vet/gofmt clean.

## Entry — 2026-06-10 — "Waive fee" is real (GPS fee waivers)

Continuing the improvement rounds (Alex: pick something and improve it, again
every 3 hours): the record's last actionable "coming soon" stub — **Waive
fee** — is now a real, supervisor-gated action. (Upload Document is the only
stub left; it needs file-storage decisions.)

**Design: no second flag.** The vendor's GPS notes already carry historical
waivers as free text, and every consumer derives waived status from
`compute.IsFeesWaived(gp_notes)` (the v0.73 regex port). So an app waiver
lives in a new `fee_waivers` extension table (migration 006 + ensureSchemaSQL;
`UNIQUE(idn)`, reason / waived_by / created_at in ET) and is **spliced into
gp_notes at the two read points** — `BuildClients` and the tracker feed
`LookupDatasets` — as a marker the regex already matches:
`GPS fees waived (app — Officer Name 2026-06-10): reason`. The record's
Fees-waived chip, the payment-status "Waived" chip, the compliance roster's
Waived flag, AND the bundled tracker's own isFeesWaived all light up through
the existing single source of truth; zero compute changes. EM-fees untouched
(record-for-record parity with the canonical skill is preserved).

Surface: record ⋯ menu → "Waive GPS fees…" (modal, optional reason) when not
waived, "Remove fee waiver" (named confirm) when app-waived — supervisor-only
in the template AND server-enforced via `requireSupervisor` (a waiver is a
money decision, same tier as field overrides). A waiver that exists only in
the vendor's notes shows the chip but no Remove action (raw text the app
can't clear — `HasFeeWaiver` distinguishes the two). `POST /admin/waiver` +
`/admin/waiver/clear` (CSRF-guarded), audited `waiver_add`/`waiver_remove`,
cache cleared on both. `fee_waivers` purged on whole-person delete
(extensionTablesByIDN); re-granting upserts by idn; clearing a non-waiver is
a quiet no-op (no audit litter on a double-submit).

Files: `internal/db/waivers.go`, `internal/handlers/waivers.go`,
`db/migrations/006_feewaivers_sqlite.sql`, splices in `internal/db/db.go` +
`internal/db/lookup_data.go`, routes in `cmd/server/main.go`, menu + modal in
`templates/console_record.html`, `AppWaiver` flag in ConsoleRecordPage.

Tests: `TestFeeWaiverLifecycle` (grant → IsFeesWaived via the BuildClients
splice → upsert keeps one row → clear → audit 2 adds / 1 remove),
`TestFeeWaiverInLookupFeed` (gp feed Notes carries the marker),
`TestFeeWaiversPurgedOnPersonDelete`, `TestFeeWaiverHandlers` (non-supervisor
403 on both endpoints through the real middleware). Live-verified in the
preview: waive → flash + chip + menu flips to Remove → `/api/lookup_data` gp
Notes shows the marker → remove → chip/marker gone, menu back to Waive; audit
shows exactly the test's 2 adds / 2 removes; no JS errors. `go build` /
`go vet` / `gofmt` clean, full `go test ./...` green.

## Entry — 2026-06-10 (round 2) — Scheduled check-ins: migration 001 §12 activated

The 3-hourly improvement round. The last dormant migration-001 table,
`scheduled_check_ins` ("calendar of upcoming check-ins", §12 — the same
situation `pinned_defendants` and `saved_searches` were in before they were
activated), is now live end-to-end: officers can **book a future check-in
appointment** from the record and see it where it matters.

**Design: display-only with respect to the compliance math.** A booking is an
appointment, not a check-in — logging the real check-in stays a separate act,
and no compute changes. State is derived at read time instead of written back:
a booking shows **✓ done** when a real check-in (raw or app-entered — both are
in `Client.CheckIns`) exists on the booked day, **⚠ missed** when the day
passed without one, neutral otherwise. The migration's
`completed_check_in_id` FK column stays unused — no fulfillment bookkeeping to
drift out of sync.

Surfaces: **record Check-ins tab** — "Schedule Check-in" button + ⋯-menu item
open a modal (date + type); bookings list in a "Scheduled check-ins" panel
(soonest first, × cancel with confirm). **Console dashboard Today's
Schedule** — bookings falling due on the as-of day appear as
"Scheduled check-in · Type" rows (distinct from the computed
"Check-in due today" compliance-window rows), attributed to the supervising
officer via officerForIDN so they survive the "My caseload" filter.

Plumbing mirrors court dates: `internal/db/schedcheckins.go`
(List/ListAll tolerant/Add/Delete via txAddWithAudit + txDeleteByID, audited
`sched_add`/`sched_delete`), `models.ScheduledCheckIn` + DefendantExtras +
LoadExtras, table mirrored into ensureSchemaSQL (already in
extensionTablesByIDN so whole-person delete purges it), officer-level routes
`POST /admin/schedule/{add,delete}`, `consoleDashboard` takes a `scheds`
param, `ConsoleSchedCI` rows on the record VM.

Tests: `TestScheduledCheckInLifecycle` (validation, chronological list,
LoadExtras carry, cancel, audit 2/1, purge on person delete),
`TestConsoleDashboardScheduledCheckIn` (today shows / tomorrow hidden / Mine
attribution), `TestConsoleRecordScheduledStates` (done / missed / future).
Live-verified in the preview: book 3 (today, past-unfulfilled, past+matching
check-in) → panel shows ✓/⚠/· states correctly → dashboard shows exactly
today's booking → cancel all 3 through the real forms → panel gone, dashboard
clean, no JS errors. go build/vet/gofmt clean, full go test ./... green.

Noted for a future round: the record's Logged check-ins panel exposes no
per-row delete even though `POST /admin/checkin/delete`
(DeleteAddedCheckIn) exists — the delete UI was lost with the classic
profile. Same for app-entered payments (endpoint exists, no console UI).

## Entry — 2026-06-10 (round 3) — Remove controls restored for app-entered rows

The 3-hourly improvement round, picking up the gap spotted while live-testing
round 2: several app-entered row types render on the console record with **no
way to remove a wrong entry**, even though the audited delete endpoints
(`/admin/note/delete`, `/admin/checkin/delete`, `/admin/courtdate/delete`)
are routed and live — that UI existed on the classic profile and was lost
with it. Tags, payments, drug screens, and scheduled check-ins already had
their × forms; **notes (Case Summary panel), app-logged check-ins (Check-ins
tab panel), and court dates (Court tab table)** did not.

All three now carry the same × confirm form pattern (CSRF + idn + id + next
back to the record). `ConsoleLoggedCI` gained the `ID` field it was missing
(without it a form would post id=0 and silently delete nothing — pinned by
the new `TestConsoleRecordRowIDs`, which asserts every removable row type
carries its DB id). The court table's action cell holds Log-outcome and ×
side by side. Scope note: only app-entered rows are removable — raw imported
check-ins/payments never render in these panels, so nothing here can touch
imported data; deletes stay officer-level (supervisor tombstone delete is the
documented backstop), all audited by the existing db layer.

Live-verified in the preview: add note + court date (plus round 2's leftover
test check-in) → all three × forms render with real ids → delete each through
the real form → correct flash each time ("Note deleted." / "Check-in
removed." / "Court date deleted."), rows gone, zero forms left, no JS errors.
go build/vet/gofmt clean, full go test ./... green.

Still open from the round-2 notes: violations and reminders have delete
endpoints but render only in the Activity timeline (no row UI) — exposing
them would need a new list section, a different-sized change. README
"Security TODOs" remains stale Azure-era text (doc-fix candidate).

## Violations & reminders row UI + "Check-in due" filter + docs refresh (2026-06-10)

"Get the website fully functional and easy to use" pass. Three changes, all
live-verified in the preview, full `go test ./...` green.

**1. The last two timeline-only row types got list panels with per-row remove.**
Violations and reminders had audited delete endpoints (`/admin/violation/delete`,
`/admin/reminder/delete`) but rendered only in the Activity timeline — the
round-5 leftover. The record's Conditions tab now lists **Recorded violations**
(severity chip via new `severityChip()` — High=risk / Medium=warn / Low=info,
description, action taken, author, × confirm form) and the Court tab lists
**Logged reminders** (body, logged/due dates, author, ×) right under the court
dates they remind about. `ConsoleRecord` gained `Violations []ConsoleViolationRow`
and `Reminders []ConsoleReminderRow`; `TestConsoleRecordRowIDs` extended to pin
both new row types' DB ids (and the severity tone + due-date formatting).
With this, EVERY app-entered row type on the record has list + remove UI.
E2E: reminder add → panel renders → delete through the new × → panel and
Activity entry both gone.

**2. The Due-Today KPI now lands on the rows it counts.** It linked to the bare
roster — useless at ~3,300 rows. New "Check-in due" filter (today / overdue) in
the roster's client-side pipeline: `due=today` matches rows whose `cis`
(next-check-in ISO sort key, same `NextDue.Deadline` source the KPI counts)
equals the page's as-of date; `due=overdue` matches the existing `ov` flag.
URL-seeded like every other filter, included in saved views (`sanitizeViewQuery`
keeps `due` — test extended), KPI href → `/console/clients?status=active&due=today`.
Parity proven live under as-of time travel (2026-03-15): KPI 7 == filtered
roster "7 of 2158 shown", all rows "Mar 15 · Active"; overdue option matches
the data's ov count; honest 0-row empty state at today on the stale fixture.

**3. Docs.** README's dead Azure-era bottom half (Python venv instructions,
App Service deploy plan, pymssql quirks, Azure security TODO checklist) replaced
with the current repo layout, the ptr1 deploy procedure, and the real security
posture; the routes paragraph now describes the console-only surface. STATUS.md
header + nice-to-have list brought current (2026-06-10).

The "Parked (revisit)" item above — the Open-Violations KPI pointing at a page
with no violations roster — was already resolved when the compliance page
gained its Violations panel (the console-only commit, 2026-06-09); noted here
so nobody re-fixes it.

## UX fit-for-purpose pass (2026-06-10)

Alex: "make sure the UX is clean and makes sense for the job the people doing
are using it for." Walked the console through the officer workflows (morning
triage, client walk-in, court prep, compliance sweep) and fixed six friction
points; all live-verified, full test suite green.

1. **Record header matches the job.** "Log Check-in" (the everyday action) is
   now the solid primary button; "Send Reminder" was the primary but is
   log-only — renamed **Log Reminder** (ghost) and the modal retitled "Log
   Court Reminder". No more implying the app sends anything.
2. **Alert feed is honest about truncation.** It caps at the 40 most-urgent
   rows, but the badge said "40" even with 1,500 underneath. Badge now shows
   the true total (new `AlertTotal` on the VM, set pre-cap) and a "Showing the
   40 most urgent of N — full rosters in Compliance" note appears when capped.
   Test `TestConsoleDashboardAlertCap`. Verified the badge total reconciles
   with the compliance page (715 = 143 behind + 570 missed + 2 violations).
3. **Terminology unified.** KPI "Overdue Check-ins (mo.)" → **"Missed
   Check-ins (this mo.)"** — same word as the roster it counts ("Missed
   Check-Ins this month") and the row chips; "overdue" now belongs solely to
   the deadline-passed roster filter.
4. **Roster officer filter has real names.** The dropdown only offered
   All/My-caseload; a supervisor couldn't pull a specific officer's caseload.
   Now lists every supervising officer (reuses `distinctOfficers`), exact-match
   in the JS pipeline, URL-seedable + saveable like every other filter.
5. **Compliance page quick filter.** One input narrows all three rosters
   (name/officer/detail), turns each panel badge into "shown of total", shows a
   global "x of y rows match", 150ms debounce for weak hardware. The live
   Missed roster runs ~1,400 rows — Ctrl+F was the only tool before. CSV
   exports unaffected (server-side, full roster).
6. **Documents tab stops pretending.** "+ Upload Document" opened a
   coming-soon toast — looked broken. Now visibly disabled with a tooltip, and
   the empty state says uploads land in a later build (keep using the shared
   location meanwhile).

Preview-cookie gotcha recorded: `ptc_asof` set during as-of testing persists
per-host in the preview browser (localhost vs 127.0.0.1 are different cookie
jars) — clear it (or check `document.cookie`) before trusting dashboard
numbers in a later session.

## Additions round: case-number search, Help page, data-freshness (2026-06-10)

Alex: "anything else you can think of adding." Three additions, each closing a
verified gap; all live-tested, full suite green.

1. **Case-number search.** `/api/lookup` matched name + IDN-prefix only —
   officers start from court paperwork that names a case. The handler now also
   scans EVERY case the person has (`caseHit` over the IDN's case list, plain
   Contains so "@1624082" and "1624082" both work). Search placeholder →
   "Search by name, IDN, or case #…"; result rows now show the case number so
   a case search visibly confirms its match. Test `TestAPILookupByCaseNumber`
   (both @-prefixed and bare queries against a fixture case).
2. **`/console/help` quick-reference.** The office is migrating off
   SharePoint+Excel and nothing in-app explained the system. Static template
   (`console_help.html`, handler `ConsoleHelp`, sidebar "?" nav entry, link
   from the `?` shortcut overlay): the daily routine (KPI cards), finding a
   client, **check-in rules incl. the both-types rule** (L1 initial-3-day /
   L2 monthly / L3+GPS weekly; in-person AND phone each cadence), chip legend
   (Behind/Missed/Past-missed/Compliant/No-referral/waived), fees table (PTR
   $20/mo; SCRAM $15/ALLIED $8/IC $0 per day), officer-vs-supervisor
   capabilities, where the numbers come from (import + app-entered merge,
   as-of control), keyboard shortcuts. Print-friendly.
3. **Data-freshness indicator.** The daily import had no in-app heartbeat — if
   it silently broke, officers worked stale numbers unknowingly.
   `sharepoint_import.py` now stamps an `import_meta` table (last_import UTC +
   mode) **inside the same transaction** as the data (dry-run rollback covers
   it). New `db.LastImport` reads it tolerantly (absent table/row/junk = no
   display — the offline fixture and pre-rollout ptr1 DB just don't show it);
   `consoleBase` formats it via new `compute.InET` into the sidebar foot:
   "Data refreshed Jun 10, 9:28 AM ET", with an amber ⚠ when >26h old.
   Tests `TestLastImport` (tolerance ladder) + live-verified fresh and stale
   stamps. NOTE: the indicator appears on ptr1 only after BOTH the next deploy
   AND the next import run; absence is normal until then.

## Deploy-readiness finish-up (2026-06-10)

Two loose ends closed before the ptr1 push:

1. **Generated letters now show on the client record.** `letter_log` rows are
   loaded with the rest of the extras (`models.LetterLogEntry`,
   `db.ListLetters`, `LoadExtras`) and merge into the Activity timeline as
   "Past-due letter generated (EM fees) — behind $X · open · by Officer".
   Read-only history (letters aren't deletable; the supervisor person-delete
   purge is the only eraser). Pinned in `TestConsoleRecordRowIDs`;
   live-verified on a smoke-DB client with logged letters.
2. **The deploy bundle now ships the importer.** `build-bundle.sh` stages
   `webapp/sharepoint_import.py`; `install-on-ptr1.sh` backs up the live copy
   then installs it to `/opt/ptr-knoxc/webapp/` (0755, app owner) — so the
   `import_meta` freshness stamp deploys in the same run, no separate scp.
   Guarded `if [ -f ]` so older bundles still install. `bash -n` clean.

Full `go test ./...` green; fresh bundle built ready for the ptr1 push.

## Stop-gap SharePoint CSV upload — `/console/import` (2026-06-10)

While the site is in testing, SharePoint stays the system of record and the
daily email import can lag it. Supervisors can now bring the DB current from
the browser: upload the four SharePoint **Export to CSV** files → dry-run
preview (per-dataset counts: new rows / field updates / unchanged / blanks
kept / CSV dups / SQL-only-kept) → Apply.

- **No new import logic.** The Go app deliberately does not write `raw_*`
  (CLAUDE.md rule); `internal/handlers/importcsv.go` shells out to the proven
  `webapp/reconcile_import.py` (natural-key match, UTC→Eastern/money/case
  normalization, NEVER deletes, empty CSV cells never blank stored values,
  idempotent). The tool gained four flags for the web path: `--adds-only`
  (insert-only mode — the page's checkbox), `--summary-json` (machine-readable
  counts the page renders), `--no-email`, and `--stamp-meta MODE` (writes the
  same `import_meta` freshness rows as the daily importer, so the sidebar
  "Data refreshed" footer reflects a web sync; mode `web-upload`).
- Flow: files staged under `<db_dir>/import_uploads/<token>/` (canonical
  names) → dry-run → Apply re-runs the same staged set for real → cache
  cleared → staging removed; abandoned previews pruned after 24 h. One import
  at a time (`importMu`); 10-min subprocess timeout.
- Routes: `GET /console/import` (supervisor), POSTs under the CSRF-guarded
  `/admin/import/{preview,apply,discard}`. Applied runs audit as
  `csv_reconcile` (run_id + headline counts) and land in the tool's
  `import_change_log` + text log as usual. Entry panel on `/console/admin`.
- Env (optional): `PYTHON_BIN`, `RECONCILE_SCRIPT` — defaults fit ptr1
  (`python3` on PATH, script relative to `WorkingDirectory=/opt/ptr-knoxc`).
  Both importer scripts now ship in the deploy bundle (they must move
  together: reconcile imports the column mapping from sharepoint_import).
- Tests: `importcsv_test.go` — supervisor gate, argv contract, preview→apply
  flow (staging, audit, cleanup), missing-file + bad-token rejection, and a
  REAL-python integration test (insert + idempotency + meta stamp; skips
  without python). `db/import_logs/` + `db/import_uploads/` gitignored (PII).
- Live-verified in the preview with the real four exports against the scratch
  DB: web preview counts == CLI dry-run exactly (+6,867 / ~1,086 / 388
  blanks-kept on the stale snapshot), apply committed, stats/footer/audit all
  updated, no console errors. Scratch DB deleted after (regenerates clean).

## "Data updated" stamp at the top of every page (2026-06-10)

Alex: "for my own sanity, put at the top of every page the last time the
website was updated." Every signed-in page now stamps when data last entered
the database (the `import_meta` row written by both the daily importer and
web CSV uploads): label + "Nh ago", amber ⚠ past 26 h, tooltip says which
path wrote it (daily import vs web CSV upload).

- **One source of truth, zero per-handler plumbing:** `Server.DataFreshness()`
  (`internal/handlers/freshness.go`, with `db.LastImportMode` added to
  meta.go) is exposed to ALL templates as the `dataFreshness` template func —
  a parse-time placeholder in `tmplFuncs()` rebound to the real method via
  `tmpl.Funcs(...)` after the Server exists (main.go). consoleBase's sidebar
  foot now reuses it too.
- Surfaces: console topbar chip (`.cfresh` in console.css; responsive — drops
  "ago"/label as width shrinks, hidden <860px where the sidebar foot still
  shows it; warn colors when stale, dark-mode aware) · tracker shell bar ·
  reports hub · printable report pages (`report.html`, `report_emfees.html` —
  ALSO in the printed `.report-head` line, so a printed roster shows its data
  currency) · audit/deleted/message/delete-confirm topbars (shared
  `freshstamp` partial in partials.html + `.freshstamp` in app.css).
- Pre-rollout DBs (no import_meta) render nothing — same tolerance as before.
- Tests: `freshness_test.go` (no-meta → fresh daily → stale web-upload; agoStr
  buckets). Live-verified: fresh chip + tooltip on console/tracker/reports/
  report-head/audit, stale 30h stamp goes amber+⚠ everywhere, dark-mode warn
  palette, mobile hides the chip with no overflow, no console errors.

## CSV-upload hardening — pre-push review fixes (2026-06-11)

A multi-agent pre-push review (`/code-review high`) of the two unpushed commits
(`8f8dc77` CSV upload page + `d932a41` freshness stamp) surfaced ~10 confirmed
findings, almost all on the stop-gap `/console/import` page. The mechanical
push-blockers are fixed here; a few by-design / low-priority items are noted at
the end. Quality gate green (gofmt/vet/build/`go test ./...`); real-python and
preview verification done.

- **Real request-body cap (was a disk-fill DoS).** `ParseMultipartForm`'s arg is
  a memory threshold, not a size limit, and the CSRF middleware parses the whole
  body *before* the supervisor check — so any logged-in officer could stream an
  unbounded body onto the DB volume. Added a global `maxBodyBytes` middleware
  (`http.MaxBytesReader`, 64 MB) ahead of auth/CSRF in `cmd/server/main.go`. The
  only large-body endpoint is the upload page; everything else is a tiny form.
- **Apply detached from the request context.** `ImportApply` ran the reconcile
  under `r.Context()`, so a client disconnect or the Cloudflare ~100 s proxy
  timeout would SIGKILL python mid-commit. Now `context.Background()` + the 10-min
  cap (single-flight `importMu` still bounds it).
- **"Committed" decided by the summary, not the exit code.** A nonzero exit
  during the tool's *post-commit* bookkeeping used to render the false "Apply
  failed (nothing committed)" while skipping the audit row and cache clear on
  already-committed data. `ImportApply` now treats a fresh non-dry-run summary as
  committed and proceeds to audit + `clearCache` regardless of a late error;
  `runReconcile` deletes any stale summary before running (the preview's dry-run
  summary shared the dir); and python writes the summary FIRST post-commit with
  the log/summary writes wrapped so they can't flip the exit code. The audit
  write is no longer fire-and-forget — a failure is logged (data is committed).
- **Skipped datasets surfaced (was a silent no-op import).** A misfiled / wrong-
  columns CSV was "skipped" with all-zero counts, yet the page showed
  "✓ Import committed" and the freshness clock was reset. The tool now returns a
  per-dataset `skipped` flag (excluded from the totals sum so the Totals row
  still unmarshals as counts), the preview + done pages show a ⚠ warning when any
  dataset was skipped, and the freshness stamp is only written when at least one
  dataset actually reconciled (an all-skipped run no longer masks stale data).
  Verified against real python: a misfiled gps file → `gps.skipped=true`, others
  still stamp; all-four misfiled → no `import_meta` stamp at all.
- **Discard / prune no longer race a running apply.** `ImportDiscard` and
  `pruneStaleStaging` now take `importMu` (TryLock) before deleting a staging
  dir, so they can't pull files out from under a concurrent apply.
- **Freshness-stamp honesty.** The console sidebar foot rendered a separate
  "Data refreshed" stamp from `consoleBase` map keys whose tooltip claimed the
  daily import was the only writer (wrong since web uploads also stamp). It now
  renders via the same `dataFreshness` func as the topbar — unified "Data
  updated" wording + a correct tooltip naming both sources — and the
  `DataRefreshed`/`DataStale` plumbing is removed from `consoleBase`.

New tests: `TestImportApplyCommittedDespiteToolError` (committed summary + late
error → done page + audit row), `TestImportSkippedDatasetWarns` (skipped flag
raises the warning). Preview live-checked: all console pages 200, both freshness
stamps render with the corrected tooltip, no JS errors.

**Deferred (by-design or low-priority, candidates for an attended pass):** a
valid upload of genuinely-old exports still stamps "now" (a valid sync *is* a
refresh — only the all-skipped no-op case is now guarded); the midday-apply
SQLite write-lock window (inherent to moving the import into business hours —
officers briefly see "database is locked" flashes); the per-render freshness
double-query (review rated trivial — single-row PK lookups under WAL); the
Windows `python3` Store-stub `LookPath` trap (dev-only; ptr1 is Linux); and the
report-header inline stamp variants (correct, just not yet a shared partial).
