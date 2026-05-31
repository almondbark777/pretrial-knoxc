# Project status — Knox County Pre-Trial (Go app)

> Handoff / checkpoint as of 2026-05-31. Single-glance "what's done, what's left."
> Deeper detail lives in `PTR_MASTER_OVERHAUL_BRIEF.md` (spec, parent folder), the
> append-only `PHASE_*.md` paper trail, `README.md`, and `deploy/DEPLOY_GO.md`.

## TL;DR

The Go rewrite is **feature-complete, tested, hardened, documented — and DEPLOYED
to `ptr1` (2026-05-31).** The Go binary is live (systemd `ptr-webapp`, `/health`
green, on the real `kh222.db`, behind cloudflared + Access). The **show-cause
letters are pending Alex's template** (framework ready; not recreated).

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
2. **Show-cause letters** *(pending Alex's template/skill — do NOT recreate)*. The
   **Behind-on-GPS report (`/reports/behind`) is the launchpad**: it's the exact list
   the letters draw from. When the template arrives, add a "Generate letters" action
   that renders one print-ready letter per behind client using *that* wording.
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
