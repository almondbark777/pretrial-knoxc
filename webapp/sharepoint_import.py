#!/usr/bin/env python3
"""
sharepoint_import.py — pull the daily SharePoint export out of a mailbox and
refresh the PTR lookup's SQLite database. No Microsoft auth on this box: a
Power Automate flow emails 4 CSV attachments (subject "PTR-EXPORT"); this
script reads them over IMAP and reloads the raw_* tables.

The CSVs come from Power Automate's "Create CSV table" (Automatic columns),
which uses SharePoint INTERNAL field names (field_1, CASE_x0023..., the
Case_x0020_Number_x003a_* projected lookups) and serializes lookup/person
columns as JSON like {"...","Value":"1587036"} or {"DisplayName":"..."}.
This script maps those internal names to the raw_* columns and unwraps the
JSON values. It ALSO accepts the older SharePoint "Export to CSV" display-name
headers, so a manual CSV drop still works.

Env (see /etc/ptr-import.env):
  IMAP_HOST/IMAP_PORT/IMAP_USER/IMAP_PASS/IMAP_FOLDER/IMAP_FROM/SUBJECT_TAG
  SQLITE_DB_PATH (default /opt/ptr-knoxc/db/kh222.db)

Run:  python3 sharepoint_import.py [--dir /folder/with/csvs] [--dry-run]
"""
from __future__ import annotations

import argparse
import csv
import email
import imaplib
import json
import os
import re
import sqlite3
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

# dataset -> (table, {db_col: [header aliases, Power-Automate-internal first]})
DATASETS = {
    "bluebook": ("raw_blue_book", {
        "sp_item_id": ["ID"],
        "idn": ["field_1", "IDN"],
        "defendant": ["field_2", "Defendant"],
        "warrant_case_num": ["CASE_x0023__x0023__x0023__x0023_", "Warrant/Case #", "Case Number", "warrant_case_num"],
        "pretrial_level": ["field_3", "Pretrial Level"],
        "referral_date": ["field_4", "Referral Date"],
        "supervision_type": ["SupervisionType", "Supervision Type"],
        "charge_type": ["field_11", "Charge Type"],
        "order_from": ["field_12", "Order From"],
        "closed_date": ["ClosedDate", "Closed Date"],
        "bond_amount": ["BondAmount", "Bond Amount"],
        "gps": ["GPS"],
        "gps_type": ["GPSType", "GPS Type"],
        "dma": ["DMAA", "DMA"],
        "birthdate": ["Birthdate", "DOB"],
        "case_status": ["field_15", "Case Status"],
        "supervising_officer": ["Supervisingofficerplaintext", "Supervising Officer"],
        "ptr_successfully_completed": ["PTRSuccessfullyCompleted_x003f_", "PTR Successfully Completed?"],
        "day_adjustment": ["DayAdjustment", "Day Adjustment"],
        "victim": ["Victim"],
    }),
    "checkins": ("raw_check_ins", {
        "sp_item_id": ["ID"],
        "idn": ["Case_x0020_Number_x003a__x0020_I", "IDN", "field_1"],
        "case_number": ["CaseNumber", "Case Number"],
        "defendant": ["Defendant"],
        "date": ["Date", "Check in Date"],
        "type_of_check_in": ["Typeofcheckin", "Type of check in"],
        "supervising_officer": ["SupervisingOfficer", "Supervising Officer"],
    }),
    "payments": ("raw_payments", {
        "sp_item_id": ["ID"],
        "idn": ["Case_x0020_Number_x003a__x0020_I", "IDN", "field_1"],
        "case_number": ["CaseNumber", "Case Number"],
        "payment_date": ["field_3", "Payment Date"],
        "payment_amount": ["field_4", "Payment Amount"],
        "payment_type": ["PaymentType", "Payment Type"],
        "officer_that_collected_payment": ["OfficerthatcollectedPayment", "Officer That Collected Payment"],
    }),
    "gps": ("raw_gps_48_hours", {
        "sp_item_id": ["ID"],
        "idn": ["Case_x0020_Number_x003a__x0020_I", "IDN", "field_1"],
        "case_number": ["CaseNumber", "Case Number"],
        "gps_type": ["Case_x0020_Number_x003a__x0020_G", "GPSType2", "GPS Type"],
        "gps_install_date": ["GPSInstallDate", "GPS Install Date"],
        "order": ["Order0", "Order"],
        "da_emailed": ["DAEmailed", "DA Emailed"],
        "victim_accept_deny_gps": ["VictimAccept_x002f_DenyGPS", "Victim Accept/Deny GPS"],
        "switched_to": ["SwitchedTo", "Switched To"],
        "switched_gps_date": ["SwitchedGPSDate", "Switched GPS Date"],
        "notes": ["Notes", "Note"],
    }),
}


def log(m): print(f"{datetime.now(timezone.utc).isoformat(timespec='seconds')}  {m}", flush=True)
def _norm(s): return re.sub(r"[\s_/#%?.\-]+", "", (s or "").lower())


def _extract(v):
    """Unwrap Power Automate lookup/person JSON to a plain string."""
    s = (v or "").strip()
    if not s:
        return ""
    if s[0] == "{":
        try:
            o = json.loads(s)
            if isinstance(o, dict):
                if o.get("Value") is not None:
                    return str(o["Value"])
                if o.get("DisplayName") is not None:
                    return str(o["DisplayName"])
        except Exception:
            pass
        return ""           # unparseable / empty lookup object
    if s == "[]":
        return ""
    return s


def _match_headers(headers, aliases_by_col):
    norm = {_norm(h): h for h in headers}
    out = {}
    for col, aliases in aliases_by_col.items():
        chosen = None
        for a in aliases:                       # exact (normalized) first, in alias order
            if _norm(a) in norm:
                chosen = norm[_norm(a)]; break
        if not chosen:                          # substring fallback
            for a in aliases:
                na = _norm(a)
                hit = next((orig for nh, orig in norm.items() if na and (na in nh or nh in na)), None)
                if hit:
                    chosen = hit; break
        if chosen:
            out[col] = chosen
    return out


def _read_csv(path):
    with path.open("r", encoding="utf-8-sig", newline="") as fh:
        rows = list(csv.reader(fh))
    if not rows:
        return [], []
    if rows[0] and (rows[0][0].startswith("ListSchema=") or '"SchemaVersion"' in rows[0][0]):
        rows = rows[1:]
    headers = [h.strip() for h in rows[0]]
    data = []
    for r in rows[1:]:
        if not any((c or "").strip() for c in r):
            continue
        data.append({headers[i]: (r[i] if i < len(r) else "") for i in range(len(headers))})
    return headers, data


def _ensure_columns(conn, table, cols):
    have = {r[1] for r in conn.execute(f"PRAGMA table_info({table})")}
    for c in cols:
        if c not in have:
            conn.execute(f"ALTER TABLE {table} ADD COLUMN [{c}] NVARCHAR(500)")
    if "sp_item_id" in cols:
        conn.execute(f"CREATE UNIQUE INDEX IF NOT EXISTS ux_{table}_sp ON {table}(sp_item_id)")


def import_dataset(conn, dataset, path, dry, mode="full"):
    table, aliases = DATASETS[dataset]
    headers, rows = _read_csv(path)
    if not headers:
        return f"{dataset}: EMPTY file, skipped (table unchanged)"
    colmap = _match_headers(headers, aliases)
    if "idn" not in colmap:
        return f"{dataset}: WARNING no IDN column matched; skipped. headers={headers[:8]}..."
    db_cols = list(colmap.keys())
    _ensure_columns(conn, table, db_cols)
    # Datasets whose source list can exceed the 5000-item page cap must NEVER be
    # wiped on a "full" run -- a full Get items only returns the first 5000 rows,
    # so a DELETE+INSERT would silently shrink an accumulating table. For those we
    # always upsert (keyed on sp_item_id) and skip the DELETE, in every mode.
    UPSERT_ONLY = {"checkins"}
    upsert = (mode == "incremental") or (dataset in UPSERT_ONLY)
    verb = "INSERT OR REPLACE" if upsert else "INSERT"
    sql = f"{verb} INTO {table} ({','.join('['+c+']' for c in db_cols)}) VALUES ({','.join('?' for _ in db_cols)})"
    if dry:
        return f"{dataset}: [{mode}{'/upsert' if upsert else ''}] would load {len(rows)} rows into {table}; cols={db_cols}"
    cur = conn.cursor()
    if not upsert:
        cur.execute(f"DELETE FROM {table}")
    n = skip = 0
    for row in rows:
        vals = [_extract(row.get(colmap[c])) for c in db_cols]
        if upsert and "sp_item_id" in db_cols and not vals[db_cols.index("sp_item_id")]:
            skip += 1; continue        # can't upsert without a key
        cur.execute(sql, vals); n += 1
    label = mode + ("/upsert" if upsert and mode != "incremental" else "")
    return f"{dataset}: [{label}] {n} rows into {table}" + (f" ({skip} skipped, no key)" if skip else "")


def find_csvs_from_dir(d):
    found = {}
    for p in Path(d).iterdir():
        if p.suffix.lower() != ".csv":
            continue
        key = _norm(p.stem)
        for ds in DATASETS:
            if ds in key:
                found[ds] = p
    return found


def fetch_csvs_from_imap(tmp, force_mode=None):
    host = os.environ["IMAP_HOST"]; port = int(os.environ.get("IMAP_PORT", "993"))
    user = os.environ["IMAP_USER"]; pw = os.environ["IMAP_PASS"]
    folder = os.environ.get("IMAP_FOLDER", "INBOX")
    sender = os.environ.get("IMAP_FROM", "").strip().lower()
    full_tag = os.environ.get("FULL_TAG", "PTR-FULL")
    delta_tag = os.environ.get("SUBJECT_TAG", "PTR-EXPORT")
    log(f"IMAP connect {host}:{port} as {user}")
    M = imaplib.IMAP4_SSL(host, port); M.login(user, pw); M.select(folder)
    # Auto mode uses only the morning's freshly-emailed export (within RECENT_HOURS
    # of this run). Manual --full / --incremental override and grab the latest of a tag.
    RECENT_HOURS = float(os.environ.get("RECENT_HOURS", "6"))
    def _latest(tag):
        typ, data = M.search(None, "SUBJECT", f'"{tag}"')
        ids = data[0].split() if data and data[0] else []
        return ids[-1] if ids else None
    def _age_hours(mid):
        typ, md = M.fetch(mid, "(BODY[HEADER.FIELDS (DATE)])")
        try:
            d = email.utils.parsedate_to_datetime(md[0][1].split(b":",1)[1].decode().strip())
            return (datetime.now(timezone.utc) - d).total_seconds() / 3600.0
        except Exception:
            return None
    mode = "full"; latest = None
    if force_mode == "incremental":
        latest = _latest(delta_tag); mode = "incremental"
    elif force_mode == "full":
        latest = _latest(full_tag) or _latest(delta_tag); mode = "full"
    else:
        # Today's batch only: a PTR-FULL emailed this morning wins (weekly reconcile),
        # else this morning's PTR-EXPORT delta. A stale full (e.g. a manual run hours
        # earlier) is older than RECENT_HOURS and is ignored, so it cannot shadow the
        # daily delta.
        fid = _latest(full_tag)
        if fid is not None:
            ah = _age_hours(fid)
            if ah is not None and ah <= RECENT_HOURS:
                latest, mode = fid, "full"
        if latest is None:
            did = _latest(delta_tag)
            if did is not None:
                ah = _age_hours(did)
                if ah is None or ah <= RECENT_HOURS:
                    latest, mode = did, "incremental"
        if latest is None:
            M.logout(); log(f"no fresh export within {RECENT_HOURS:.0f}h; nothing to do"); sys.exit(0)
    if latest is None:
        M.logout(); raise SystemExit("No PTR-FULL or PTR-EXPORT message found")
    typ, md = M.fetch(latest, "(RFC822)")
    msg = email.message_from_bytes(md[0][1])
    frm = email.utils.parseaddr(msg.get("From", ""))[1].lower()
    if sender and sender not in frm:
        M.logout(); raise SystemExit(f"Latest message is from {frm}, not allowed sender {sender}")
    log(f"mode={mode}; using message dated {msg.get('Date','?')} subject={msg.get('Subject','')}")
    found = {}
    for part in msg.walk():
        fn = part.get_filename()
        if not fn:
            continue
        key = _norm(Path(fn).stem)
        for ds in DATASETS:
            if ds in key:
                out = tmp / f"{ds}.csv"
                out.write_bytes(part.get_payload(decode=True) or b"")
                found[ds] = out
    try:
        M.store(latest, "+FLAGS", "\\Seen")
    except Exception:
        pass
    M.logout()
    log(f"Fetched {sorted(found)}")
    return found, mode


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir")
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--full", action="store_true", help="force full wipe+reload")
    ap.add_argument("--incremental", action="store_true", help="force upsert (delta)")
    ap.add_argument("--db", default=os.environ.get("SQLITE_DB_PATH", "/opt/ptr-knoxc/db/kh222.db"))
    args = ap.parse_args()
    force = "full" if args.full else ("incremental" if args.incremental else None)
    db = Path(args.db)
    if not db.exists():
        log(f"FATAL: DB not found at {db}"); return 2
    with tempfile.TemporaryDirectory() as td:
        tmp = Path(td)
        if args.dir:
            csvs = find_csvs_from_dir(args.dir); mode = force or "full"
        else:
            csvs, mode = fetch_csvs_from_imap(tmp, force_mode=force)
        if not csvs:
            log("FATAL: no recognizable CSV attachments found"); return 3
        conn = sqlite3.connect(str(db), timeout=30)
        conn.execute("PRAGMA journal_mode=WAL"); conn.execute("PRAGMA busy_timeout=30000")
        try:
            conn.execute("BEGIN")
            for ds in ("bluebook", "checkins", "payments", "gps"):
                log(import_dataset(conn, ds, csvs[ds], args.dry_run, mode))
            if args.dry_run:
                conn.execute("ROLLBACK"); log("dry-run: rolled back, no changes written")
            else:
                conn.commit(); log("commit OK")
        except Exception as e:
            conn.execute("ROLLBACK"); log(f"ERROR: rolled back: {e}"); return 1
        finally:
            conn.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
