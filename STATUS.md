# Project status — Knox County Pre-Trial (Go app)

> Handoff / checkpoint as of 2026-05-31. Single-glance "what's done, what's left."
> Deeper detail lives in `PTR_MASTER_OVERHAUL_BRIEF.md` (spec, parent folder), the
> append-only `PHASE_*.md` paper trail, `README.md`, and `deploy/DEPLOY_GO.md`.

## TL;DR

The Go rewrite is **feature-complete, tested, hardened, documented — and DEPLOYED
to `ptr1` (2026-05-31).** The Go binary is live (systemd `ptr-webapp`, `/health`
green, on the real `kh222.db`, behind cloudflared + Access). The **show-cause
letters are now built** (Past-Due EM Fees report) — a faithful port of Alex's
`past-due-em-fees` skill that reuses his own memo template; parity-proven
record-for-record against the canonical Python. **Data entry is also live in the
code** (Phase 10): add clients, payments, and check-ins from the website, stored
app-side and merged into every view (importer-proof). Neither Phase 9 nor 10 is
redeployed to ptr1 yet.

> **Deployed 2026-05-31** via `deploy/install-on-ptr1.sh` (bundle scp'd + run on the
> box; key-auth set up for `alex@ptr1`). Pre-swap backups of the binary, unit, and DB
> are in `/opt/ptr-knoxc/backups/`. `SUPERVISOR_EMAILS` set (the 6 + alexander.bentley),
> `IMPORTER_RETIRED=false`, `COOKIE_SECURE=true`. The systemd unit points at the
> current `kh222.db` (rename not done). Rollback = restore the saved unit + restart.

---

## ✅ Done (this overhaul, all committed & pushed to `main`)

**Core rewrite**
- [x] Single Go binary (pure-Go SQLite, no CGO); native SQLite, no T-SQL shim.
- [x] Business math ported server-side (`internal/compute`) — parity-proven vs. the
      canonical JS by unit + DB golden tests.
- [x] Two-gate auth: Cloudflare-Access header + 12h session cookie + Basic fallback.

**Landing / structure (Alex's call: keep the tracker primary during transition)**
- [x] `/` serves the **existing client tracker** (untouched bundle in an iframe),
      fed by a Go reimplementation of `/api/lookup_data` (honors tombstones/overrides).
- [x] New admin/data-entry app at `/dashboard`, reached via a top-bar button;
      every new page has a "← Client Tracker" link back.

**Data entry (Phase 10) — add records from the website**
- [x] **Add a client** (`/admin/add_defendant`, "+ Add client" on the dashboard),
      **add payments** and **add check-ins** to an existing client (profile forms).
- [x] App-owned tables (`added_defendants/payments/check_ins`) **merged into every
      read path** (BuildClients, the tracker feed, EM-fees) — importer-proof, so an
      app-entered record shows + computes everywhere and survives the Sunday reload.
      Tombstones suppress them; every write audited. (Officer-level; supervisor
      delete is the backstop.) Paper trail: `PHASE_10_DATAENTRY.md`.

**Admin & data-entry (Phase 7)**
- [x] **Importer-proof delete**: `deleted_idns` tombstone filtered in `BuildClients`
      → person/case vanishes from every view and stays gone across the Sunday reload.
      Flips to a physical `raw_*` delete via `IMPORTER_RETIRED` at SharePoint cutover.
- [x] Restore (un-tombstone); whole-person and single-case granularity.
- [x] Supervisor-gated **field overrides** (`overrides` table, applied after the raw
      read, flagged in the UI).
- [x] Officer CRUD: notes, tags, court dates, reminders, violations.
- [x] **Every write audited** in ET → viewable at `/admin/audit` (global + per-person).
- [x] Supervisor tier via `SUPERVISOR_EMAILS`; allow-list via `ALLOWED_EMAILS`.

**Read-side features**
- [x] Dashboard (stats + Behind/Missed rosters), case grid, analytics.
- [x] Per-client calendar **and roster (team) calendar**.
- [x] **My Day** — each officer's own caseload worklist (due / behind / missed).
- [x] Profile **Case Info panel** with MISSING critical-field badges (Brief 2.7).
- [x] Live lookup search.

**Reports / export**
- [x] **CSV export** for the Behind/Missed rosters and the full case grid.
- [x] **Printable reports** (`/reports`): print-ready Behind-on-GPS and Missed
      reports (letterhead + clean table; `@media print` → black-on-white document).
- [x] **Past-Due EM Fees / show-cause letters** (`/reports/em-fees`) — faithful Go
      port of Alex's `past-due-em-fees` skill (Phase 9): 5+ days behind on GPS fees,
      Open/Closed split, totals + spot-check highlights, per-client memo (.docx) +
      whole-batch zip, all rendered from the office's **own template** (reused, not
      recreated). Parity-proven record-for-record vs. the canonical Python; the
      generated memo is field-for-field identical to the skill's.

**UX & hardening**
- [x] Modern dark design system; responsive/mobile; accessibility (toast `role=status`,
      labeled icon buttons); Wong palette kept for status (icon + color).
- [x] **CSRF** tokens on all admin POSTs; **Secure-cookie** toggle (`COOKIE_SECURE`);
      security headers (nosniff, `X-Frame-Options: SAMEORIGIN`, Referrer-Policy).

**Quality / ops / docs**
- [x] 34 test functions (compute, db, handlers, auth) — `go vet`/`gofmt`/`go test` green.
- [x] `deploy/DEPLOY_GO.md` cutover guide + `deploy/smoke.sh` post-deploy check.
- [x] `README.md` rewritten as the Go-app front door; `webapp/.env.example` documents
      all env vars; `.gitattributes` enforces LF.

---

## ⬜ What still needs to be done

1. **✅ DONE — Deploy to `ptr1`** (2026-05-31). Live via `deploy/install-on-ptr1.sh`.
   Still worth doing soon: **Phase 5 automated backups** on `ptr1` (only a manual
   pre-deploy DB copy exists so far — the open 🔴), and the
   `kh222.db → pretrial_release.db` rename (cosmetic; the unit currently points at
   `kh222.db`). Verify the UI + roster counts on the live data.
2. **✅ DONE — Show-cause letters** (Past-Due EM Fees, Phase 9). Built from Alex's
   `past-due-em-fees` skill: methodology ported to `internal/emfees`, his
   `memo_template.docx` embedded and filled in Go, served at `/reports/em-fees`
   (per-memo + zip + CSV). Parity-proven. **Still to do:** redeploy the binary to
   ptr1 so it goes live there, and verify counts on real ptr1 data. Supervisor
   **field overrides are now spliced into the EM-fee read** (a corrected GPS
   type/rate, name, case status, or referral/closed date reaches the report + the
   show-cause letters — consistent with every other view). Optional later: a
   CSV-upload path for ad-hoc runs (and to pick up Switched-To/COURT columns the
   daily import doesn't carry); xlsx export.
3. **Validate on real data**. The offline `db/kh222.db` is a stale snapshot, so its
   numbers (esp. missed-check-in counts) are NOT representative — re-check rosters and
   "feel" against live `ptr1` data after deploy.
4. **Two-server HA** *(production scale-up)*. Design locked in `PHASE_8_HA.md`:
   **rqlite, 3 nodes** (two app servers + a tiny witness) + Cloudflare LB failover.
   Bounded code change (queries already native SQLite; only the connection layer +
   the importer's write path move). Do at the end of the testing phase.

### Nice-to-have / optional (not blocking)
- Roster-calendar weekly/column totals; per-officer split on the Behind report.
- Drug-screen logging (table + CRUD) — was on the old Python roadmap.
- "Undo last delete" one-click on the dashboard (restore already exists at `/admin/deleted`).
- DB-backed allow-list (currently env/`ALLOWED_EMAILS` with a built-in fallback).

---

## Hard rules honored throughout (Brief Part 5.4)
Native SQLite only · no writes to `raw_*` except the `IMPORTER_RETIRED` path ·
`/health` always auth-free · importer & timers untouched · reuse
`compute.CaseTokens` / `FmtOfficer` / ET timestamps · same dark Wong-palette theme.
