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
Tag / Pin / Override / Waive remain demo-safe "coming soon" stubs this pass; the clients
table also has a hover **"✓ Check-in"** quick-action that persists. The roster's
**bulk-select** bar (appears on selection) offers **Export selected** (real — client-side
CSV of the chosen rows) and **Check-in selected** (demo-safe modal; the bulk write path is
deferred). Selection is keyed by IDN so it survives paging and sort, and is cleared when a
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
   pre-edit HTML at `_tracker_work/PTR_Client_Lookup.BEFORE.html` (gitignored). **Still TODO (dev-only,
   non-gating): update `tools/parity_ref.py` to the both-types rule (it's the golden-value generator,
   not shipped).**

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
- Pin/unpin clients (no endpoint yet). *(Bulk select + Export-selected is now built; the bulk-check-in write path is still a demo-safe stub.)*
- Add a `channel` column to `reminders` (currently folded into the body) when a real SMS/email provider is wired.
- Promote roles/conditions/templates from placeholders to real config tables.
