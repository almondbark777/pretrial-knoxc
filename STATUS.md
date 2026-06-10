# Project status — Knox County Pre-Trial (Go app)

> Last updated 2026-05-31. Single-glance "what's done, what's left."
> Deeper detail lives in `PTR_MASTER_OVERHAUL_BRIEF.md` (spec, parent folder), the
> append-only `PHASE_*.md` paper trail, `README.md`, and `deploy/DEPLOY_GO.md`.

## TL;DR

The Go rewrite is **feature-complete, tested, hardened, documented, deployed, and
monitored.** The Go binary is live on `ptr1` (systemd `ptr-webapp`, `/health` green,
`kh222.db`, behind cloudflared + Access). **Show-cause letters** (Phase 9, EM fees)
and **website data entry** (Phase 10, add clients/payments/check-ins) are both live
on the box. **Automated daily backups** (Phase 5) run at 11:45 UTC to the dedicated
backup drive. **Netdata monitoring** is running with phone push alerts via ntfy.

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

**Landing / structure**
- [x] `/` serves the **existing client tracker** (untouched bundle in an iframe),
      fed by a Go reimplementation of `/api/lookup_data` (honors tombstones/overrides).
- [x] The **Case Console** (`/console`) is the app UI. The classic `/dashboard`
      interface was **removed 2026-06-09** (Alex: "get rid of the classic
      interface") — `/dashboard`, `/client_profile.html?idn=`, `/calendar.html`,
      `/my_day.html`, `/pretrial_app.html`, `/analytics.html`, and
      `GET /admin/add_defendant` all 302 to their console equivalents (query
      params carried over; pinned in `cmd/server/main_test.go`). The printable
      `/reports` pages and supervisor utilities (`/admin/{delete,deleted,audit}`)
      remain, re-chromed to link only tracker/console. The console record gained
      the classic profile's last exclusives: a Drug Screens tab, and supervisor
      "Audit history" / "Delete client…" menu actions.

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

**Read-side features** *(now all served by the console — the classic pages that
first carried them were removed 2026-06-09; My Day's role is covered by the
console dashboard's "My caseload" scope toggle)*
- [x] Dashboard (stats + Behind/Missed rosters), case grid, analytics.
- [x] Per-client calendar **and roster (team) calendar**.
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
- [x] 34+ test functions (compute, db, handlers, auth, metrics) — `go vet`/`gofmt`/`go test` green.
- [x] `deploy/DEPLOY_GO.md` cutover guide + `deploy/smoke.sh` post-deploy check.
- [x] `README.md` rewritten as the Go-app front door; `webapp/.env.example` documents
      all env vars; `.gitattributes` enforces LF.

**Monitoring & backups (Phase 5 + ops)**
- [x] **Automated daily backups** to dedicated drive (`/dev/sda1` ext4, 916 GB, UUID
      `7a6a0d0a-...`, mounted `/mnt/backup/ptr`). WAL-safe online snapshot via Python
      stdlib; `integrity_check` on every backup; 30-day retention. Timer: 11:45 UTC
      (~30 min after daily import). On-box restore proof: `raw_blue_book` 3959,
      `raw_check_ins` 5000, `raw_payments` 2826, `raw_gps_48_hours` 778. Phase 5 🔴 closed.
- [x] **Netdata** host+service dashboard (CPU/RAM/disk/IO + ptr systemd units). Bound
      to `127.0.0.1:19999`; view via SSH tunnel or Cloudflare Access.
- [x] **`/metrics` endpoint** (Prometheus-text, auth-free, localhost-only) — request
      counts, latency histogram, in-flight, uptime, goroutines, memory. Scraped by
      Netdata go.d prometheus job.
- [x] **Phone push alerts** via ntfy (topic `ptr-alerts-kc2847xq`) — Netdata alarms
      (down service, disk/RAM pressure) delivered to phone. No account required.

---

## ⬜ What still needs to be done

1. **Validate on real data.** The offline `db/kh222.db` used for testing is a stale
   snapshot — re-check roster counts, EM-fee report totals, and general "feel" against
   live `ptr1` data now that the full binary is deployed.
2. **`kh222.db → pretrial_release.db` rename** (cosmetic). Update
   `Environment=DB=` in `ptr-backup.service` and `Environment=SQLITE_DB_PATH=` in
   the webapp unit at the same time. See `PHASE_5_BACKUP.md` §5.
3. **Two-server HA** *(production scale-up)*. Design locked in `PHASE_8_HA.md`:
   rqlite 3-node + Cloudflare LB failover. Do at the end of the testing phase.

### Nice-to-have / optional (not blocking)
- [x] Roster-calendar weekly/column totals — **done 2026-06-09**: both team calendars
      (`/calendar.html` + `/console/calendar`) now have a trailing week-total column
      and a weekday-totals footer row + month grand-total cell. Same numbers as the
      day cells, re-aggregated server-side (`rosterCalendarMonth`); reconciliation
      pinned in `TestRosterCalendarMonth`.
- [x] Per-officer split on the Behind report — **done 2026-06-09**:
      `/reports/behind?by=officer` renders one section per supervising officer
      (alphabetical, Unassigned last) with a count + behind-$ subtotal; toggle
      link flips between flat and grouped views, print-ready like the flat
      report. Subtotals reconcile with the flat roster (pinned in
      `TestGroupBehindByOfficer` + live check: 141 clients / $97,822 both views).
- [x] Drug-screen logging (table + CRUD) — **done 2026-06-09**: `drug_screens`
      extension table (migration 005 + EnsureSchema self-provision), officer
      CRUD (date / test type / result / substances / notes), color-coded
      results, every write audited, rows purged on whole-person delete.
      CSRF-guarded `/admin/drugscreen/{add,delete}`. UI lives on the **console
      record** (Drug Screens tab + Record-Drug-Screen modal + "Last Drug
      Screen" summary field + Activity-timeline merge) since the classic
      profile was removed the same day. Tests:
      `internal/db/drugscreens_test.go`, `TestDrugScreenChip`,
      `TestConsoleRecordDrugScreens` + live e2e add/render/delete check.
- [x] "Undo last delete" one-click — **done 2026-06-09** (rebuilt for the console;
      the old classic-dashboard branch is superseded): supervisor ↩ button on the
      console Admin tombstone panel restores the newest tombstone
      (`POST /admin/undo_last_delete`, CSRF, audited via Restore*). Test
      `TestUndoLastDelete` + live e2e delete→undo check.
- [x] Pin/star clients — **done 2026-06-09**: per-officer pins
      (`pinned_defendants` from migration 001, now also self-provisioned),
      `POST /admin/pin/toggle` (audited), Pin/Unpin on the record's ⋯ menu +
      "📌 Pinned" badge, and a Pinned Clients quick-list strip on the console
      dashboard. Purged on whole-person delete. Tests: `TestPinToggle`,
      `TestPinsPurgedOnPersonDelete`, `TestPinnedRows`.
- [x] Saved views — **done 2026-06-09**: per-officer named roster filter combos
      (`saved_searches` from migration 001, finally in use). "★ Save view" on
      `/console/clients`, one-click chips, owner-scoped delete, sanitized
      query params, audited. Tests: `TestSavedViews`, `TestSanitizeViewQuery`.
- DB-backed allow-list (currently env/`ALLOWED_EMAILS` with a built-in fallback).

---

## Hard rules honored throughout (Brief Part 5.4)
Native SQLite only · no writes to `raw_*` except the `IMPORTER_RETIRED` path ·
`/health` always auth-free · importer & timers untouched · reuse
`compute.CaseTokens` / `FmtOfficer` / ET timestamps · same dark Wong-palette theme.
