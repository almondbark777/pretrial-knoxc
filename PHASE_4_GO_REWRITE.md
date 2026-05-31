# Phase 4 — Go Rewrite

> **Phase 4 tracking doc** (see Brief Part 0.1 / Part 4). Append-only.
> Read `PHASE_2_PARITY_MATRIX.md` (golden values) and `PHASE_3_FIXES.md` first.

---

## Entry — 2026-05-30 · Model: claude-opus-4-8 (1M context) · Effort: high (autonomous)

Per Brief Part 0.2 (LOCKED), the Go rewrite **re-implements the business math
server-side** (`computeCheckIns` / `computePTRFees` / `computeGPS`) and serves
server-rendered pages, retiring the embedded client-side HTML tool. This entry
records the structure built, the ports, and the **passing parity tests**.

### Toolchain note

Go was not installed at session start. It is now installed system-wide at
`C:\Program Files\Go` (go1.26.3, via the elevated winget MSI). All builds/tests
below were run with that toolchain. `go build`/`go test` work from any new shell.

---

## What was built

```
pretrial-knoxc/
├── go.mod / go.sum                 module "pretrial-knoxc" (chi, gorilla/sessions, modernc.org/sqlite)
├── cmd/server/main.go              entry point: chi router, templates, sessions, tz embed
├── internal/
│   ├── compute/                    ★ business math — faithful port of the canonical JS
│   │   ├── compute.go              ParseDay, ParseLevel, ComputeCheckIns, ComputePTRFees, ComputeGPS
│   │   ├── helpers.go              FmtOfficer, TodayET (America/New_York)
│   │   ├── notes.go                StripHtml, IsFeesWaived
│   │   └── compute_test.go         18 golden/branch tests — ALL PASS
│   ├── db/
│   │   ├── db.go                   native SQLite; BuildClients() joins raw_* (mirrors lookup_datasets+buildClients)
│   │   └── golden_test.go          DB-backed §4 golden test against db/kh222.db — PASS
│   ├── auth/auth.go                Cloudflare-Access header + 12h session cookie + Basic fallback, 22-user allow-list
│   ├── handlers/                   thin handlers (handlers.go) + roster/stats service (service.go)
│   └── models/models.go            HTTP-facing shapes
├── templates/                      login.html, index.html (dashboard+rosters), profile.html
├── static/app.css                  colorblind-safe palette (vermillion/sky/amber per spec §2.9)
└── deploy/ptr-webapp-go.service    systemd unit for the single binary
```

The single binary built at **19.9 MB**, no CGO, no external runtime deps.

---

## Ported functions (canonical JS → Go), line-checked against the spec

| JS (`8a6913e5-*.js`) | Go (`internal/compute`) | Notes |
|---|---|---|
| `_parseDay` / `_parseDayImpl` | `ParseDay` | noon-UTC `Date.UTC(y,m-1,d,12,0,0)` → `Noon(y,m,d)` in UTC; ISO + US regexes, then fallback layouts |
| `parsePretrialLevel` | `ParseLevel` | same regexes; `null` modeled as `(0,false)` |
| `_mondayOfWeek/_firstOfMonth/_lastOfMonth/_addDays` | `mondayOfWeek/firstOfMonth/lastOfMonth/addDays/nextMonth` | `Weekday()` Sunday=0 matches JS `getUTCDay()` |
| `computeCheckIns` | `ComputeCheckIns` | initial 3-day, L1 short-circuit, L2 monthly, L3/GPS/unknown weekly, future-window guard, `effEnd=min(track,closed)` |
| `computePTRFees` | `ComputePTRFees` | `$20×months`, L1 flat $20, payment filter `(?i)\bptr\b` (word-boundary, not `LIKE '%PTR%'`) |
| `computeGPS` | `ComputeGPS` | vendor/rate, switch-aware billing (`$23` switch day), GPS-relief cap (strict `<`), day-adj at post-switch rate, surplus with `-ceil()` on the negative branch |
| `_vendorOf/_rateOf/_isReliefSwitch` | `vendorOf/rateOf/isReliefSwitch` | identical substring/regex semantics |
| `isFeesWaived/stripHtml` | `IsFeesWaived/StripHtml` | `/waiv/i` AND `/(fee\|gps\|payment\|charge)/i` |
| `_fmt_officer` (Python) | `FmtOfficer` | email → display name; free-text passthrough matches Python `capitalize()` |
| EST "today" (`Intl…America/New_York`) | `TodayET` | tz embedded via `import _ "time/tzdata"` so it works on any host |
| `buildClients` + `lookup_datasets` | `db.BuildClients` | raw_* join by IDN; install-nonempty GPS row wins; dates pre-parsed; tolerant of optional columns |

**Native SQLite only** — the T-SQL `sqlglot` shim is gone (Brief 5.4).

---

## Test results (all green)

```
go vet ./...            # clean
go test ./...
  ok  internal/compute  (18 tests)   ParseDay, ParseLevel, L1/L2/L3/closed check-ins,
                                      L1-flat/L2/L3/closed PTR, PTR payment filter,
                                      SCRAM/ALLIED/IC/switch/relief/surplus/IN-CUSTODY GPS
  ok  internal/db        (1 test)    DB-backed §4 golden test against db/kh222.db
```

Every **PHASE_2 §4 golden value** is asserted and reproduced:

| Client | Assertion | Result |
|---|---|---|
| JONES 1704942 (L1) | PTR owed $20 | ✅ |
| REASONOVER 1426070 (L2) | 5 windows / 3 missed / $100 | ✅ |
| HANCOCK 1704989 (L3) | 5 windows / 5 missed / $40 | ✅ |
| COLLINS 1704603 (closed L2) | 1 window / 0 missed / $20 | ✅ |
| AGUILAR 1386687 (SCRAM) | 41 days / $615 / −41 surplus days | ✅ |
| PIETY 1340291 (ALLIED) | 33 days / $264 / −33 surplus days | ✅ |
| switch SCRAM→ALLIED | 14×15 + 23 + 16×8 = $361 | ✅ |
| relief "no gps" day 10 | capped → $150 | ✅ |
| surplus paid-ahead | $65 → 5 days | ✅ |

### Live HTTP smoke test (binary, against `db/kh222.db`)

- `GET /health` → `{"db":"up","ok":true}` (auth-free) ✅
- `GET /` unauthenticated → `303` redirect to `/login` ✅
- `GET /api/lookup?q=AGUILAR` (Basic auth) → correct hit ✅
- `GET /api/clients?idn=1386687` → full **server-side** bundle: GPS SCRAM $15 × 41 = $615 owed, surplus −615 / −41 days; PTR Apr+May = $40; weekly check-in windows — **matches the golden values exactly** ✅

---

## Parity status vs the Phase 2 / Part 3.2 matrix

Every **business-math** row of the Part 3.2 matrix is now reproduced server-side
in Go and proven by test (see table above). Specifically green in Go:

- Level parse, initial grace, L2 monthly, L3/GPS weekly, future-window guard,
  closed-case stop, L1 monthly-roster exclusion (in `missedCheckInsRoster`).
- PTR $20×months, L1 flat, **word-boundary PTR filter** (no `LIKE '%PTR%'`).
- GPS rates, **+1 inclusive days**, switch-aware billing, **relief freeze (strict `<`)**,
  day adjustment at post-switch rate, surplus$ and **±ceil surplus days**.
- IN CUSTODY → unknown vendor → MISSING (not a false `IC` match).
- EST "today"; IDN join key; case tokenization `[,\s]+` (write paths fixed in Phase 3).
- Cross-client BehindRoster (surplus<0, A–Z) and MissedCheckInsRoster
  (open, current month, L1-excluded, 3-day grace) computed server-side.

The Go app does **not** carry the Y1 server-side drift (flat 14-day rule): it
implements the real per-level windows instead, as required.

### R1 (GPS switch/relief/waiver columns) in Go

`db.BuildClients` reads `switched_to` / `switched_gps_date` / `notes` when the
columns exist and degrades cleanly when they don't (same data dependency flagged
in Phase 2/3). The compute layer is fully tested for switch/relief/waiver, so the
moment those columns carry data on `ptr1`, the Go app bills them correctly. **Same
[verify on ptr1] action as Phase 3 applies.**

---

## Build & deploy

```bash
# Build (Linux amd64 single binary)
GOOS=linux GOARCH=amd64 go build -o server ./cmd/server

# Deploy to ptr1
scp ./server alex@ptr1:~
ssh alex@ptr1 'sudo install -m0755 ~/server /opt/ptr-knoxc/server'
# Ship templates/ + static/ alongside (APP_BASE_DIR=/opt/ptr-knoxc):
scp -r templates static alex@ptr1:~ && \
  ssh alex@ptr1 'sudo cp -r ~/templates ~/static /opt/ptr-knoxc/ && sudo chown -R ptrapp:ptrapp /opt/ptr-knoxc/{templates,static,server}'

# Swap the service (keeps cloudflared, Access policy, import timer untouched):
sudo cp deploy/ptr-webapp-go.service /etc/systemd/system/ptr-webapp.service
sudo systemctl daemon-reload && sudo systemctl restart ptr-webapp
curl -s http://127.0.0.1:8000/health
```

`webapp/.env` already supplies `APP_PASSWORD` / `APP_SESSION_SECRET`. Set
`SQLITE_DB_PATH` (the unit defaults to `pretrial_release.db`).

---

## Done vs. remaining

**Done (this session):**
- ✅ Server-side business math, fully ported + unit/DB/HTTP tested for parity.
- ✅ Native-SQLite data layer (`raw_*` join), no T-SQL shim.
- ✅ Two-gate auth (Cloudflare Access header + session + Basic), 22-user allow-list.
- ✅ Routes: `/`, `/login`, `/api/login`, `/api/logout`, `/client_profile.html`,
  `/api/lookup`, `/api/clients`, `/api/refresh`, `/health`; static; 60s TTL cache.
- ✅ Server-rendered dashboard (stats + BehindRoster + MissedCheckInsRoster) and
  client-profile (check-ins/PTR/GPS with MISSING + waiver banners).
- ✅ systemd unit; `.gitignore` for Go artifacts; deploy steps.

**Remaining (follow-on — explicitly out of this session's scope):**
- ⬜ Full visual parity of the legacy multi-page UI (`analytics.html` charts,
  full case-management grid, calendar grid view). The data/compute is ready;
  this is template work.
- ⬜ Write/CRUD endpoints (referral/check-in/payment/notes/tags/court-dates/
  reminders/violations) — the Python `app.py` surface. Port with the
  Phase-3-corrected `[,\s]+` tokenizer; write to extension tables only.
- ⬜ Run `db/migrations/001_app_extensions_sqlite.sql` against the SQLite DB so
  the extension tables exist for those endpoints.
- ⬜ DB rename `kh222.db → pretrial_release.db` (coordinated; Phase 1 finding) —
  the unit already points at the new name.
- ⬜ Phase 5 backups (the open 🔴 from Phase 1) and Phase 6 sign-off.

### Next step
Phase 5 (backups) — still the only 🔴 blocking production (Phase 1). Then Phase 6
sign-off. The Go compute core is parity-proven and ready to grow the remaining
routes/templates onto.

---

## Entry — 2026-05-30 (recheck) · Model: claude-opus-4-8 (1M context) · Effort: high

Independent recheck of Phases 1–4 (verifying claims against the code, the
canonical v0.82 JS spec, and the offline DB — not trusting the prior entries).
**Verdict: Phases 1–3 hold up; the Phase 4 compute core is genuinely
parity-proven, but the recheck found one substantive gap + several nuances in
the Go rewrite that the entry above does not flag.** None affect the *live* app
(still the embedded HTML tool); all are about the not-yet-deployed Go binary.

### Re-verified (all green)

- `go build`/`go vet` clean; **all 19 tests pass** (18 compute + 1 DB golden);
  Linux amd64 cross-build produces the single binary (~19.5 MB).
- Compute math traced line-for-line vs `assets/8a6913e5-*.js`: strict-`<` relief
  cap, `dBefore*old + 23 + dAfter*new` switch billing, `-ceil()` surplus,
  word-boundary PTR filter — all faithful.
- **Golden values reproduced 3 independent ways** (canonical JS read by hand,
  `tools/parity_ref.py` against `db/kh222.db`, and the Go golden test) — exact
  match incl. LUTTRELL −460/15 → **−31** (confirms `-ceil()`, not `floor`).
- Appendix B #7 absent: `raw_check_ins` has separate `date` + `referral_date`;
  LUTTRELL check-ins are 2026 dates, referral `4/27/2025`. No `LIKE '%PTR%'`.
- Phase 3 fixes present in code: R2 at `queries.py:513,746` (`re.split(r"[,\s]+")`);
  R1 at `queries_ext.py:127` (`r.get(snake)` comprehension).
- Auth: `Cf-Access-Authenticated-User-Email` trusted, 22-email allow-list, 12h session.

### Gaps found (Go rewrite only — pre-production; fix before Phase 6 swap-in)

| ID | Severity | Finding | Location |
|---|---|---|---|
| **G1** | 🟠 **Medium** | **Multi-case defendants collapsed to one blue_book row ("last wins").** Canonical/live keeps every row + lets the officer pick a case. Offline data: **47 IDNs multi-row — 16 differ in pretrial level, 46 in referral date, 29 mix open/closed status.** Affects per-client compute (wrong level/ref for those clients) AND roster membership (dashboard filters on the single collapsed status). Golden tests use single-row clients, so they don't catch it. | [`db.go:203`](internal/db/db.go:203) |
| **G2** | 🟡 Low | **`caseFilter` dropped** from `ComputeCheckIns`/`ComputePTRFees`/`ComputeGPS` (JS takes it). Not exposed yet (profile shows whole-client totals) so no wrong number today, but a real spec gap (Brief 2.5/2.6 per-case narrowing) — tied to G1. | `internal/compute/compute.go` (signatures) |
| **G3** | 🟡 Low | **BehindRoster status filter differs.** Canonical component defaults to **open-only** (`5037ee28:618`); Go `behindRoster` counts **all statuses**. Matches Brief 2.8 *text* ("every client surplus<0") but not the component default → dashboard "Behind GPS" count can differ. Decide which is intended. | [`service.go:18`](internal/handlers/service.go:18) |
| **G4** | 🟡 Latent | **`missedCheckInsRoster` caps "checked this month" at `track`, not `monthEnd`.** No effect at `track=today`; diverges from spec for historical as-of dates. Also the `ref > monthEnd` skip is omitted but is mathematically subsumed by the grace check (no behavioral difference). | [`service.go:59`](internal/handlers/service.go:59) |
| **G5** | ℹ Scope | **Route surface is a subset** — no `/pretrial_app.html`, `/analytics.html`, `/api/stats`, `/api/defendants`, calendar. Already listed under "Remaining" above, but it means Brief 4.8's "all pages load / full Part 3 matrix green against Go" bar is **not yet met** — only the *compute* rows are. | `cmd/server/main.go` routes |

### Minor cross-doc notes

- Repo `CLAUDE.md` is the **stale pre-overhaul memory** (Azure SQL / pymssql /
  FastAPI-on-Azure) — superseded by the brief but still in the tree; could
  mislead a future session.
- Canonical spec version is **v0.82 (SQL-connected)** in the repo/phases vs
  v0.81 in the brief text — a documented bump, business rules unchanged.
- Go default DB path is `pretrial_release.db` (not yet existing on ptr1); needs
  `SQLITE_DB_PATH` or the coordinated rename before swap-in (already noted above).

### Recommended next actions

1. **G1 + G2 together:** restore multi-case handling — keep all blue_book rows
   per IDN and add the `[,\s]+` `caseFilter` to the three compute funcs, with an
   open-status-preferred rep for roster dedup (mirror `5037ee28` BehindRoster/
   MissedCheckInsRoster). Add a multi-case client to the golden tests.
2. **G3:** confirm intended Behind-GPS scope (open-only vs all) and align.
3. Carry G1–G4 into the **Phase 6** verification subagent's checklist.

---

## Entry — 2026-05-30 (recheck fixes G1–G4) · Model: claude-opus-4-8 (1M context) · Effort: high

Actioned G1–G4 from the recheck entry above. `go vet` clean, `go build` clean,
**all 22 tests pass** (20 compute + 2 DB), Linux amd64 single binary builds, and
a live HTTP smoke test against `db/kh222.db` confirms the golden values are
unchanged after the refactor.

### ⚠ Correction to the recheck entry's G1 numbers

The G1 row above said "**29 mix open/closed status**." That was **wrong** — it
counted multi-row IDNs with *differing `case_status` strings*, but in `kh222.db`
those differences are only **capitalization** (`'OPEN'` 1652 / `'Open'` 550 /
`'open'` 4); **there are zero closed rows in the offline copy.** Verified by
`SELECT case_status, COUNT(*) … GROUP BY case_status`. So:

- **The offline open/closed roster-membership effect is 0** (no closed rows to
  mis-pick as the rep). The concrete offline impact of the collapse is the
  **16 differing-pretrial-level** IDNs (compute used one case's level).
- The open-vs-closed rep choice **still matters on `ptr1`'s live data**, which
  *does* have closed cases (Phase 1; Brief Closed Date semantics). The fix is
  correct and forward-looking; the offline severity was overstated.

### G1 — keep every case per IDN (no more "last row wins")

`db.BuildClients` now returns **`map[string][]*compute.Client`** — one client
object per blue_book row, all sharing the IDN's check-ins/payments and the
install-nonempty GPS record (mirrors canonical `buildClients`, which maps one
client per bb row). `clients[idn] = append(...)` replaces `clients[idn] = c`.
[`db.go:203`](internal/db/db.go:203)

Roster/stats dedup now uses **`openRep(cases)`** — first open-status case, else
the first — mirroring the canonical BehindRoster/MissedCheckInsRoster dedup
(open-preferred). [`service.go`](internal/handlers/service.go). New DB test
`TestMultiCaseRetained` (real DB): **48 multi-case IDNs, 16 differing-level now
retained, 0 open/closed-mix** (offline) — and asserts the open-preferred rep
wherever a status mix exists (will exercise on ptr1).

### G2 — `caseFilter` on `ComputePTRFees` + `ComputeGPS`

Added the spec's `caseFilter` param (the JS signatures take it). Payment
`Payment.Case` is now populated from `raw_payments.case_number`; a new
`matchCase` tokenizes both sides on `/[,\s]+/` (lowercased) — verified both
`warrant_case_num` and `payment.case_number` use the same `@`-prefixed
comma/space-joined form. Empty filter = whole-client (identical to before, so
all golden values hold). `computeCheckIns` intentionally takes **no** caseFilter
(neither does the canonical — per-case behavior comes from which case row anchors
the windows). Handlers accept `?case=` on `/client_profile.html` and
`/api/clients`; `selectCase` picks the matching case row (or the open rep) and
the filter. New unit tests `TestPTR_CaseFilter` / `TestGPS_CaseFilter` prove the
paid side narrows while GPS owed/days (single install record) stays constant.

### G3 — BehindRoster now open-default

`behindRoster` dedups among GPS-active cases (open-preferred rep) and includes
only **open** reps — matching the canonical component's default `'open'` filter
and making it consistent with `missedCheckInsRoster` (also open-only). Closed/All
views can be added later if a UI toggle is wanted. [`service.go`](internal/handlers/service.go)

### G4 — month-boundary fix

`missedCheckInsRoster`'s "checked this month" test now spans
`monthStart..monthEnd` (full calendar month) instead of capping at `track`,
matching the canonical `d >= monthStart && d <= monthEnd`. No effect at
`track=today`; correct for historical as-of dates. [`service.go`](internal/handlers/service.go)

### Still open

- **G5** (legacy multi-page UI: `pretrial_app.html`, `analytics.html`,
  `/api/stats`, `/api/defendants`, calendar) — deliberately **not** done here;
  it's a large template/UI surface, not a correctness gap. Still the main
  remaining work before Brief 4.8's "all pages load" bar.
- ~~A **case-selector UI** on the profile page~~ — **DONE this session**: the
  profile renders a case `<select>` (multi-case clients only) that navigates to
  `?case=<token>`; verified live (1099283: 3 case options; selecting `129027`
  switches the displayed case row). `templates/profile.html` + `caseOptions()`.
- Cross-doc: stale `CLAUDE.md` (now carries a SUPERSEDED banner pointing to the
  brief); DB rename `kh222.db → pretrial_release.db` still pending.

### Test results

```
go vet ./...        # clean
go test ./...
  ok  internal/compute  (20 tests)  + TestPTR_CaseFilter, TestGPS_CaseFilter
  ok  internal/db        (2 tests)  TestGoldenAgainstRealDB, TestMultiCaseRetained
```
Live smoke (binary vs `db/kh222.db`): `/health` ok · `/` unauth → 303 ·
AGUILAR `/api/clients` still SCRAM $15×41=$615, surplus −41 · multi-case IDN
resolves · `?case=` plumbs through (1099283: 3 case options; selecting `129027`
switches the displayed case row). Golden math unchanged by the refactor. Added
**LUTTRELL** (the 418-day client) to the DB golden test: SCRAM 418d / $6270 /
−$460 / **−31** days — passes.

### Independent verification (fresh-context subagent, from the spec — not these notes)

A separate agent re-derived the trickiest cells from `8a6913e5-*.js` + `db/kh222.db`
and checked the Go: **all PASS** — switch billing `14×15+23+16×8=361`; LUTTRELL
surplus `−ceil(460/15) = −31` (not −30); L3 first week = Mon **5/4** for HANCOCK
(ref 4/30 → initialDeadline 5/3 → mondayOfWeek 4/27 → +7); relief cap is strict
`<` (compute.go `lt`); case-filter empty/tokenize/empty-row semantics match the JS.

**One documented edge (unreachable today):** the JS `_matchCase` has an
`if (!col) return true` branch — if the payments dataset had **no case column at
all**, a non-empty filter would include every row. The Go `matchCase` collapses
`(row, col)` to a pre-resolved string, so a *missing column* surfaces as
`rowCase == ""` → **excluded** under a filter (JS would include). Not exercised:
`raw_payments` always has `case_number`, so the column is always present and the
empty-*value* case is handled identically. Left as-is (matching the real schema)
rather than plumbing column-existence through `Payment` for an impossible-in-prod
edge; flagged here for completeness.

### Next step
Phase 5 (backups) remains the only 🔴 blocking production. Phase 6 sign-off
should re-run parity against the Go app and confirm G1–G4 on **ptr1's live data**
(where closed cases actually exist).

---

## Entry — 2026-05-30 (G5 — remaining UI built) · Model: claude-opus-4-8 (1M context) · Effort: high

Built the remaining Go UI surface (the G5 item flagged as the last gap to Brief
4.8's "all pages load"). Server-rendered in the existing minimal dark style; no
external JS/CDN deps. `go vet` clean, all tests still pass, Linux binary builds,
live smoke test of every page green.

### Added

| Route | Page / payload |
|---|---|
| `GET /pretrial_app.html` | **Case-management grid** — one row per IDN (open-preferred rep), sortable by the live filter box (name/IDN/officer); columns Level, Status, Officer, GPS (vendor + `behind` badge), Missed (this-month badge + total count), PTR balance. KPI cards up top. |
| `GET /analytics.html` | **Analytics** (active/open roster) — KPI cards + dependency-free **CSS bar charts** by pretrial level, GPS vendor, and officer caseload (top 12); GPS/PTR owed+paid totals. |
| `GET /calendar.html?idn=&month=&case=` | **Per-client month calendar** — ported `getEventsForClient` to Go (`internal/compute/events.go`): referral, GPS install/switch, closed, check-ins (in-person/phone/other split), payments (GPS vs PTR split), missed + due windows. Prev/next month nav; honors `?case=`. |
| `GET /api/stats` | Dashboard `Stats` JSON. |
| `GET /api/defendants` | The case-management bundle (`[]DefendantRow`) JSON. |

Nav links wired across Dashboard/Cases/Analytics; profile page links to the
calendar. New compute: `compute.GetEventsForClient` (+`Event`). New service:
`defendantRows` (behind/missed flags reuse the roster fns — no divergence),
`analyticsData`, `calendarMonth`. New models: `DefendantRow`, `Analytics`, `Bar`,
`CalEvent`, `CalDay`.

### Smoke test (binary vs `db/kh222.db`)

All pages 200, no template errors. `/api/stats` = `{total:2157, open:2157,
gpsActive:346, behindGps:131, missedThisMonth:1399}`. Grid badge counts match the
stats exactly (131 behind / 1399 missed). Analytics bars scale correctly
(L3=100%, L1=79%, L2=45%). Calendar renders real per-day events for LUTTRELL.

> ⚠ **The offline `kh222.db` numbers are NOT representative** — it's a stale,
> sparse snapshot (check-ins capped/old), so `missedThisMonth`=1399/2157 is a
> data artifact (most clients have no recent check-in), not a logic issue. The
> roster logic was verified against the canonical spec separately. Real counts
> come from ptr1.

### Not built (intentionally deferred)

- **Roster-mode calendar** (aggregated counts per day/week/month across all
  clients) — Brief 2.9's second calendar mode. Per-client mode is done.
- **Write/CRUD endpoints** (referral/check-in/payment/notes/violations) — the
  Python `app.py` write surface; port with the `[,\s]+` tokenizer to extension
  tables only. Still the remaining functional gap besides Phase 5/6.
- Visual polish / charting libraries — kept dependency-free CSS bars by choice.
