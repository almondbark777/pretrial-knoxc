# Case Console (`/console`) — second dashboard (Direction B)

> Append-only paper trail, matching the `PHASE_N_*.md` convention. Built 2026-05-31 →
> 2026-06-01 (Opus 4.8 1M). Brief: `PROMPT_new_dashboard_for_claude_code.md` (parent
> folder). Functional spec: `PTR_Dashboard_BUILD_SPEC.md`. Layout ref:
> `Pretrial-UI-Mockup.html`. Demo walkthrough + real-vs-demo-safe map: `CONSOLE_DEMO_RUNBOOK.md`.
> **Branch `feature/console-dashboard`; uncommitted by design.**

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
