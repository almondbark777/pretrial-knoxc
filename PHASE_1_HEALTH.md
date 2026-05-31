# Phase 1 — Server Health Snapshot

> **Phase 1 tracking doc** (see WORKFLOW.md / Brief Part 0.1).
> Generated 2026-05-30 · Model: claude-sonnet-4-6 · Effort: interactive (command-by-command with user on ptr1).
> Read before Phase 2 (parity audit).

---

## Summary

All critical services are running. No errors in application logs. Database is
clean and healthy. One **red** item (no backups configured on attached drive)
and several **yellow** items documented below.

---

## 1. Service status

| Service | Status | Notes |
|---|---|---|
| `ptr-webapp` | ✅ GREEN — active (running) since 2026-05-30 12:17:58 UTC | Restarted at 12:17 UTC today (likely manual code-update deploy); no errors since |
| `cloudflared` | ✅ GREEN — active (running) since 2026-05-29 18:45:52 UTC | All prechecks passed: DNS, UDP, TCP, Cloudflare edge |
| `ptr-import.timer` | ✅ GREEN — active (waiting); next trigger 2026-05-31 11:14 UTC | 11:14 UTC ≈ 7:14 ET — matches the 7:10 ET target |
| `ptr-import.service` | ✅ GREEN — inactive (dead), last run 2026-05-30 11:12:47 UTC, exit 0 | Full import completed successfully; see row-count section |

---

## 2. System resources

| Resource | Value | Assessment |
|---|---|---|
| Uptime | 1 day 17 min (rebooted ~2026-05-29 00:35 UTC) | ✅ Stable |
| Disk (`/`) | 98 GB total, 7.9 GB used (9%), 86 GB free | ✅ No risk |
| RAM | 14 GiB total, 837 MiB used, 14 GiB available | ✅ Very healthy |
| Swap | 4 GiB, 0 in use | ✅ Not swapping |
| Load average (1/5/15 min) | 0.11 / 0.04 / 0.01 | ✅ Essentially idle |

**Note:** `/opt`, `/var`, and `/` are all on the same LVM volume — no separate
mounts. Disk is not partitioned per-service. Adequate for now.

---

## 3. Application

- **Module:** `app_lookup:app` (i.e., `webapp/app_lookup.py`) — confirmed in
  the systemd `ExecStart` line and in live log PIDs. The legacy `app.py` exists
  in the repo but is **not** what is running.
- **Workers:** 2 uvicorn worker processes.
- **Memory:** 150 MiB (peak 159 MiB) — well within available RAM.
- **Health endpoint:** `GET /health` → `{"ok":true,"db":"up"}` ✅
- **Errors in logs:** None. All entries are `INFO`-level 200/307 responses.
- **Active traffic pattern:** user at 76.237.14.249 hits `/` then
  `/api/lookup_data` — the main PTR Client Lookup flow.

---

## 4. Database

**File:** `/opt/ptr-knoxc/db/kh222.db`

| Check | Result | Assessment |
|---|---|---|
| `PRAGMA integrity_check` | `ok` | ✅ No corruption |
| `PRAGMA wal_checkpoint(FULL)` | `(0, 68, 68)` — 0 busy writers, 68/68 pages checkpointed | ✅ WAL fully flushed |
| WAL file size before checkpoint | 274 KB (`kh222.db-wal`) | ✅ Normal for active WAL mode |

### Row counts (post-import, 2026-05-30 11:12 UTC)

| Table | Rows | Notes |
|---|---|---|
| `raw_blue_book` | 3,955 | ⚠️ See note below — 7-row discrepancy vs import message |
| `raw_check_ins` | 5,000 | ⚠️ At cap — expected (see note) |
| `raw_payments` | 2,826 | ✅ |
| `raw_gps_48_hours` | 777 | ✅ |
| `defendants` | 25,769 | ✅ (includes historical master list) |
| `cases` | 6,245 | ✅ |
| `check_ins` (normalized) | 2,600 | ⚠️ Lags raw — see note |
| `payments` (normalized) | 1,947 | ⚠️ Lags raw — see note |
| `gps_events` (normalized) | 337 | ⚠️ Lags raw — see note |
| `audit_log` | 0 | (app not in active use yet) |
| `court_dates` | 0 | (same) |
| `defendant_notes` | 0 | (same) |
| `defendant_tags` | 0 | (same) |

---

## 5. Import run (2026-05-30 11:12 UTC)

Import ran in **`mode=full`** on a Saturday.

```
mode=full; using message dated S…R-FULL
bluebook:  [full]         3948 rows → raw_blue_book
checkins:  [full/upsert]  5000 rows → raw_check_ins
payments:  [full]         2826 rows → raw_payments
gps:       [full]         776  rows → raw_gps_48_hours
commit OK
```

**⚠️ Yellow — full mode ran on Saturday.** The brief specifies: incremental
(PTR-EXPORT) Mon–Sat, full (PTR-FULL) Sundays. Today is Saturday but Power
Automate sent a PTR-FULL email. This is an operational anomaly (Power Automate
trigger, not a code issue), but worth noting. The import handled it correctly —
it acted on the email subject it found.

---

## 6. Findings

### 🔴 RED — No backups configured on attached drive

The server has a second drive connected for backups. **No backup jobs have been
set up.** The SQLite database (`kh222.db`, 8.3 MB today, growing daily) has no
local or offsite copy. A drive failure or accidental `rm` would permanently
destroy all accumulated check-in history, payment records, and GPS data.

**Action required before production use:** configure Phase 5 (backups) —
nightly `sqlite3 .backup` or `cp` of the DB file to the second drive, with a
weekly `PRAGMA integrity_check` verification and restore-test.

---

### ⚠️ YELLOW — DB file not yet renamed

Current path: `/opt/ptr-knoxc/db/kh222.db`
Brief specifies: `/opt/ptr-knoxc/db/pretrial_release.db`

The rename hasn't happened. The systemd service, import script, and app all
hard-reference the current name. The rename should be done as a coordinated
step (stop service → rename → update all references → restart) before the Go
rewrite, so the Go binary starts with the canonical path.

---

### ⚠️ YELLOW — raw_blue_book row-count discrepancy (3955 vs 3948)

The import logged "3948 rows into raw_blue_book" (full mode = wipe + reload),
but the table has 3,955 rows afterward. If the wipe ran first, the count should
be exactly 3,948. The extra 7 rows may indicate:
- The importer isn't doing a full DELETE before INSERT for blue book, or
- A prior import left rows whose `sp_item_id` didn't appear in today's export
  (deleted SharePoint items that aren't being pruned).

This needs investigation in Phase 2 — specifically, whether
`sharepoint_import.py`'s "full" mode for blue book is actually doing a
`DELETE FROM raw_blue_book` before the bulk insert, or just upserting.

---

### ⚠️ YELLOW — raw_check_ins at 5000-row cap

`raw_check_ins` has exactly 5,000 rows — the SharePoint export cap. Per the
brief, this is expected behavior. The importer uses upsert (not wipe) for
check_ins, which is correct.

**Prerequisite to verify:** the SharePoint "Check Ins" list must have the
`Modified` column indexed (List settings → Indexed columns → Modified).
Without that index, the daily delta filter throws a 5000-item threshold error.
This hasn't been verified yet.

---

### ⚠️ YELLOW — Normalized tables lag raw tables

| Table | Normalized | Raw | Gap |
|---|---|---|---|
| check_ins | 2,600 (max rowid 4,646) | 5,000 | 2,400 |
| payments | 1,947 (max rowid 3,660) | 2,826 | 879 |
| gps_events | 337 (max rowid 1,035) | 777 | 440 |

The high max_rowid vs count gap indicates these tables have been cleared and
re-seeded at least once in the past.

**Impact:** The app currently uses `app_lookup.py`, which serves
`GET /api/lookup_data` from `queries_ext.lookup_datasets()` — this reads
directly from `raw_*` tables, so the primary PTR Client Lookup feature is
unaffected.

However, any webapp routes that query the normalized tables
(`check_ins`, `payments`, `gps_events`) via the old `queries.py` T-SQL path
may show stale or incomplete data. Phase 2 should confirm which routes (if any)
in `app_lookup.py` read from normalized vs raw tables.

---

### ⚠️ YELLOW — No sqlite3 CLI installed

`sqlite3` binary is not on the system PATH. All DB diagnostics must go through
Python (`/opt/ptr-knoxc/venv/bin/python3 -c "import sqlite3 ..."`). This is
mildly inconvenient for manual investigation. Consider:
```
sudo apt install sqlite3
```

---

## 7. ptr1_diag.sh (created this session)

`tools/ptr1_diag.sh` was written during this phase for repeatable future health
checks. Run it from ptr1 (requires `ptrapp` sudo access or equivalent):

```bash
bash tools/ptr1_diag.sh
```

---

## 8. Next step

Proceed to **Phase 2 — Parity audit** (`PHASE_2_PARITY_MATRIX.md`).
Key pre-audit actions from this phase:
1. Install `sqlite3` CLI (convenience, not blocking).
2. Investigate the 7-row blue_book discrepancy in `sharepoint_import.py`.
3. Verify the SharePoint "Check Ins" Modified column is indexed.
4. **Block on Phase 5 (backups) before production use** — no backup currently exists.
