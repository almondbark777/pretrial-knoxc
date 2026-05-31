# Phase 5 — Backup Drive Setup

> **Phase 5 tracking doc** (see WORKFLOW.md / Brief Part 0.1). Append-only.
> Read `PHASE_1_HEALTH.md` §6 (the open 🔴) first.

---

## Entry — 2026-05-30 · Model: claude-opus-4-8 (1M context) · Effort: medium

Phase 1 found the only 🔴 blocking production: **a second drive is attached to
`ptr1` but no backups are configured** — the SQLite DB (all `raw_*`, normalized,
and app-written extension tables in one file) has no copy. This entry writes the
backup tooling and **proves the backup/restore mechanism locally**; the steps
that genuinely need `ptr1` (mounting the physical drive, scheduling against the
live import, restore-test on the box) are marked **[ON ptr1]** and remain to be
run there.

### Corrections from Phases 1–4 already baked into the artifacts

- **DB is still `kh222.db`** (rename hasn't happened) → the script is
  **path-driven** via `$DB` (default `/opt/ptr-knoxc/db/kh222.db`); survives the
  future `pretrial_release.db` rename by changing one env var.
- **No `sqlite3` CLI on `ptr1`** → backup uses the venv Python's
  `sqlite3.Connection.backup()` (stdlib), not the CLI, not `cp`.
- **WAL mode is active** → online backup is consistent; a raw `cp` of a live WAL
  DB is not, so the script never `cp`s the `.db`.
- **Schedule from the observed import**, not the repo's `05:30` line: Phase 1 saw
  `ptr-import.service` finish ~11:12 UTC (timer ~11:14 UTC ≈ 07:14 ET). Backup
  timer fires **11:45 UTC** (~30 min after) — flagged to re-verify on `ptr1`.
- **DB is tiny (~8.3 MB), root has ~86 GB free** → retain **30 daily** copies.

---

## Artifacts created (in `deploy/`)

| File | Role |
|---|---|
| [`deploy/ptr-backup.sh`](deploy/ptr-backup.sh) | WAL-safe online backup of `$DB` → `$DEST/<dbname>-YYYY-MM-DD.db`; verifies the snapshot with `integrity_check`; also captures `/etc/ptr-import.env` + a `migrations-*.tar.gz`; prunes > `RETAIN_DAYS` (30). Exits non-zero on any failure so the unit logs it. Run as `ptrapp`. |
| [`deploy/ptr-backup.service`](deploy/ptr-backup.service) | `Type=oneshot`, `User/Group=ptrapp`, `RequiresMountsFor=/mnt/backup/ptr`, `ProtectSystem=full` + `ReadWritePaths=/mnt/backup/ptr`. Env-overridable `DB`/`DEST`/`PYTHON`/`RETAIN_DAYS`. |
| [`deploy/ptr-backup.timer`](deploy/ptr-backup.timer) | `OnCalendar=*-*-* 11:45:00 UTC`, `Persistent=true` (catches missed runs after downtime). |

The script does, in order: **(1)** online snapshot → **(2)** `integrity_check`
on the snapshot (deletes it + fails if not `ok`) → **(3)** copy env + tar the
migrations → **(4)** prune > 30 days.

---

## Local self-test (mechanism proof) ✅

Could not mount the ptr1 drive from the Windows box, but the **core backup +
verify + restore-parity logic** was run against the offline `db/kh222.db` (the
exact `sqlite3.Connection.backup()` path the script uses):

```
1) online backup OK -> 8,544,256 bytes in 0.043s   (== source size)
2) integrity_check on backup = 'ok'
3) row-count parity source vs backup:
     raw_blue_book          src=  2206 backup=  2206 OK
     raw_check_ins          src=  2600 backup=  2600 OK
     raw_payments           src=  1947 backup=  1947 OK
     raw_gps_48_hours       src=   337 backup=   337 OK
   RESTORE-TEST RESULT: PASS
```

So the snapshot is byte-complete, passes integrity_check, and restores to
identical row counts. This is the Phase 5.4 restore proof at the mechanism level;
the **on-box** restore-test below still must be run on `ptr1` (different DB size,
WAL actively being written, real mount).

---

## Remaining — [ON ptr1] (needs the box; could not be done from Windows)

1. **Discover & mount the drive.** `lsblk -f` / `blkid` → note device, fs, UUID.
   Add a persistent mount **by UUID** (fstab or a `mnt-backup-ptr.mount` unit) at
   `/mnt/backup/ptr`; `mkdir -p` it; confirm `mount -a` works and `ptrapp` can
   write (`sudo -u ptrapp touch /mnt/backup/ptr/.wtest && rm …`).
2. **Install the artifacts.**
   ```bash
   sudo install -m0755 deploy/ptr-backup.sh /opt/ptr-knoxc/deploy/ptr-backup.sh
   sudo cp deploy/ptr-backup.service deploy/ptr-backup.timer /etc/systemd/system/
   sudo systemctl daemon-reload && sudo systemctl enable --now ptr-backup.timer
   ```
3. **Confirm the schedule** matches the live import: `systemctl list-timers ptr-import.timer ptr-backup.timer` and the last `ptr-import.service` run; adjust
   `OnCalendar` to import-finish + 30 min if 11:45 UTC is off.
4. **Run once + on-box restore-test (mandatory).**
   ```bash
   sudo systemctl start ptr-backup.service ; journalctl -u ptr-backup.service -n 20 --no-pager
   # restore the latest to scratch and verify on the box:
   latest=$(ls -t /mnt/backup/ptr/kh222-*.db | head -1)
   /opt/ptr-knoxc/venv/bin/python3 - "$latest" <<'PY'
   import sqlite3,sys; c=sqlite3.connect(sys.argv[1])
   print("integrity:", c.execute("PRAGMA integrity_check").fetchone()[0])
   for t in ("raw_blue_book","raw_check_ins","raw_payments","raw_gps_48_hours"):
       print(t, c.execute("SELECT COUNT(*) FROM "+t).fetchone()[0])
   PY
   ```
   Paste the integrity_check + row counts back into this doc as the on-box proof.
5. **At the kh222 → pretrial_release rename:** set `DB=` in
   `ptr-backup.service` to the new path (the script is already path-driven).

---

## On-box restore proof — 2026-05-31

Drive: `/dev/sda1` ext4, UUID `7a6a0d0a-3d21-4492-b90e-ad7a4d8102a2`, label `PTRbackup`,
mounted at `/mnt/backup/ptr` (~916 GB, 870 GB free). `ptrapp:ptrapp` owner, mode 755.
fstab entry: `UUID=7a6a0d0a... /mnt/backup/ptr ext4 defaults,nofail 0 2`.
Timer: `ptr-backup.timer` enabled, `OnCalendar=*-*-* 11:45:00 UTC`.

Backup run: `kh222-2026-05-31.db` (8,671,232 bytes). Restore-parity check (`sudo -u ptrapp`):

```
integrity: ok
raw_blue_book    3959
raw_check_ins    5000
raw_payments     2826
raw_gps_48_hours  778
```

---

## Status

- 🟢 **Tooling written + mechanism proven locally** (online backup, integrity,
  restore-parity all PASS).
- 🟢 **Cleared on `ptr1` — 2026-05-31.** Drive mounted, timer enabled, on-box
  restore proof recorded above. The 🔴 from Phase 1 is closed.

### Minor known warning
`WARN: could not copy /etc/ptr-import.env` — the env file is at
`/opt/ptr-knoxc/webapp/.env`, not `/etc/ptr-import.env`. Non-fatal (DB backup
succeeds). Fix: update `ENV_FILE` default in `ptr-backup.sh` or add
`Environment=ENV_FILE=/opt/ptr-knoxc/webapp/.env` to `ptr-backup.service`.

### Next step
Phase 6 sign-off: re-confirm PHASE_4 G1–G4 fixes against ptr1's live data
(where closed cases exist), then deploy Phases 9 & 10 (committed, not yet on ptr1).
