#!/usr/bin/env python3
"""
reconcile_import.py — NON-DESTRUCTIVE reconcile of SharePoint "Export to CSV"
files against the PTR SQLite raw_* tables, with a full change log.

Unlike sharepoint_import.py (which can DELETE+reload on a --full run), this tool
NEVER deletes a row. For each CSV row it:
  * INSERTs it if no matching row exists in SQL  (logged as ADD)
  * UPDATEs only the individual fields that differ (logged as CHANGE, old->new)
  * leaves it untouched if every mapped field already matches (no log)
Rows that exist in SQL but not in the CSV are KEPT and only reported, never removed.

Matching has no SharePoint item ID to rely on (the Export-to-CSV files omit it),
so rows are matched on a per-dataset NATURAL KEY built from stable stored fields:
  bluebook : idn + warrant_case_num
  gps      : idn + case_number + gps_type
  payments : idn + payment_date + payment_amount + payment_type + case_number
  checkins : idn + date + type_of_check_in + case_number + supervising_officer
A handful of event rows are byte-identical across every stored field; those share
a key and cannot be told apart without an ID (~1% of payments/check-ins). They are
collapsed to one and noted in the run summary.

Change log is written TWICE (THREE places with --email):
  * DB table  import_change_log  (queryable: per run_id / idn / dataset)
  * a text file <db_dir>/import_logs/reconcile-<UTC>.log  (human-readable summary + detail)
  * optional emailed report (--email / REPORT_TO): the run SUMMARY only (counts,
    no PII). The full detail log is attached ONLY with --attach-log (it carries
    IDNs + names). SMTP reuses the IMAP mailbox creds unless overridden.

Column mapping (display-name + Power-Automate aliases) is reused verbatim from
sharepoint_import.py, so both tools agree on how CSV headers map to db columns.

Email env (all optional; defaults derive from the IMAP/ptr-import.env values):
  REPORT_TO=ops@example.com[,second@...]   recipients (or pass --email-to)
  SMTP_HOST (default: IMAP_HOST with imap->smtp, else smtp.gmail.com)
  SMTP_PORT (default 587 STARTTLS; 465 uses implicit SSL)
  SMTP_USER / SMTP_PASS (default: IMAP_USER / IMAP_PASS)
  REPORT_FROM (default: SMTP_USER)

Run:
  python3 reconcile_import.py --dir /folder/with/csvs [--db PATH] [--dry-run]
  python3 reconcile_import.py --dir /folder --email --email-to ops@knox... [--attach-log]
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sqlite3
import sys
import urllib.request
from collections import defaultdict
from datetime import datetime, timedelta, timezone
from pathlib import Path

# Reuse the canonical column mapping + CSV reader from the main importer.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from sharepoint_import import DATASETS, _read_csv, _match_headers, _extract, _norm, log  # noqa: E402

# Per-dataset natural key (db-column names). Chosen from collision analysis on
# real data: bluebook & gps are collision-free; payments/checkins have ~1% of
# rows that are identical in every stored field and unavoidably share a key.
KEYS = {
    "bluebook": ["idn", "warrant_case_num"],
    "gps":      ["idn", "case_number", "gps_type"],
    "payments": ["idn", "payment_date", "payment_amount", "payment_type", "case_number"],
    # supervising_officer dropped from the check-in key: it's wildly inconsistent in
    # the CSV ('LOVELESS' / 'Kathy Jones' / '') and not needed for uniqueness.
    "checkins": ["idn", "date", "type_of_check_in", "case_number"],
}

ORDER = ("bluebook", "checkins", "payments", "gps")

# Default recipient for the emailed report when neither --email-to nor REPORT_TO
# is set. Override either way to send elsewhere.
DEFAULT_REPORT_TO = "support@retrobiscuit.org"


def _cv(v):
    """Canonical comparison form of a value (whitespace-normalized string)."""
    return (str(v) if v is not None else "").strip()


def canon(v):
    """Like _extract, but also unwrap simple JSON arrays so a multi-choice value
    stored raw as ["Pre-Trial"] compares/writes equal to the display form
    'Pre-Trial' (and ["GPS","SCRAM"] -> 'GPS, SCRAM'). Prevents cosmetic
    array-vs-text differences from being logged as real changes."""
    s = _extract(v)
    if len(s) >= 2 and s[0] == "[" and s[-1] == "]":
        try:
            o = json.loads(s)
            if isinstance(o, list):
                parts = []
                for x in o:
                    if isinstance(x, dict):
                        parts.append(str(x.get("Value", x.get("DisplayName", "")) or ""))
                    else:
                        parts.append(str(x))
                return ", ".join(p for p in parts if p).strip()
        except Exception:
            pass
    return s


# Date formats seen across the data (CLAUDE.md: "ISO-with-Z, US with time, ISO
# without tz, junk"). Output canonical 'YYYY-MM-DD' or 'YYYY-MM-DD HH:MM' so the
# SQL side and the CSV side compare/key EQUAL instead of every date looking like a
# change or a brand-new row.
#
# CRUCIAL: the live SQL (email-importer-seeded) stores timestamps in UTC with a
# trailing 'Z' (e.g. a check-in at 12:50 Eastern is '...T16:50:00Z'); the CSV
# Export-to-CSV shows site-local EASTERN time with no tz. So a real timestamp must
# be converted UTC->Eastern before comparing, or check-ins/payments never match
# (proven: 5000 orphans). BUT pure dates are stored as midnight UTC
# ('2026-05-21T00:00:00Z' meaning "May 21"); converting those to Eastern would
# wrongly roll them back a day -- so midnight-UTC is treated as a pure date and NOT
# shifted. Non-midnight UTC (incl. SharePoint's local-midnight placeholder stored
# as '04:00:00Z') IS converted.
_DATE_FORMATS = (
    "%Y-%m-%d %H:%M:%S", "%Y-%m-%dT%H:%M:%S", "%Y-%m-%d %H:%M", "%Y-%m-%dT%H:%M",
    "%Y-%m-%d",
    "%m/%d/%Y %I:%M:%S %p", "%m/%d/%Y %I:%M %p",
    "%m/%d/%Y %H:%M:%S", "%m/%d/%Y %H:%M", "%m/%d/%Y",
    "%m/%d/%y %H:%M:%S", "%m/%d/%y %H:%M", "%m/%d/%y",
)


def _nth_sunday(year, month, n):
    d = datetime(year, month, 1)
    first_sun = 1 + (6 - d.weekday()) % 7     # weekday(): Mon=0..Sun=6
    return first_sun + 7 * (n - 1)


def _utc_to_eastern(dt):
    """Convert a naive UTC datetime to naive US Eastern, handling DST without any
    tzdata dependency (EDT -4 from 2nd Sun Mar 02:00 local to 1st Sun Nov 02:00,
    else EST -5). Thresholds expressed in UTC."""
    y = dt.year
    edt_start = datetime(y, 3, _nth_sunday(y, 3, 2), 7)    # 02:00 EST == 07:00 UTC
    edt_end = datetime(y, 11, _nth_sunday(y, 11, 1), 6)    # 02:00 EDT == 06:00 UTC
    off = -4 if (edt_start <= dt < edt_end) else -5
    return dt + timedelta(hours=off)


def canon_date(s, dateonly=False):
    """Parse a value as a date/datetime -> canonical 'YYYY-MM-DD[ HH:MM]', or None
    if not a date. UTC 'Z' timestamps are converted to Eastern (see module note);
    midnight-UTC is kept as a pure date. dateonly drops the time entirely."""
    z = (s or "").strip()
    if not z:
        return None
    is_utc = z[-1] in "Zz"
    z = z.rstrip("Zz").strip()
    m = re.search(r"([+-])(\d{2}):?(\d{2})$", z)           # explicit tz offset
    if m:
        if int(m.group(2)) == 0 and int(m.group(3)) == 0:
            is_utc = True
        z = z[:m.start()].strip()
    z = re.sub(r"(\d{1,2}:\d{2}:\d{2})\.\d+", r"\1", z)     # strip fractional secs
    for fmt in _DATE_FORMATS:
        try:
            dt = datetime.strptime(z, fmt)
        except ValueError:
            continue
        has_time = ("%H" in fmt) or ("%I" in fmt)
        # Always convert a UTC timestamp to Eastern. (Earlier a midnight-UTC guard
        # tried to protect "pure dates", but real referral/check-in times legitimately
        # land at midnight UTC = 8pm Eastern, so the guard mis-shifted them. True
        # date-only fields aren't stored with a 'Z' so they never reach here.)
        if is_utc and has_time:
            dt = _utc_to_eastern(dt)
        if dateonly:
            return dt.strftime("%Y-%m-%d")
        if dt.hour or dt.minute:
            return dt.strftime("%Y-%m-%d %H:%M")
        return dt.strftime("%Y-%m-%d")
    return None


# Key columns where the time component is a meaningless SharePoint placeholder
# (payments always export '<date> 1:00'); key on the DATE ONLY so a date-only SQL
# value matches a date+placeholder-time CSV value. Check-in times ARE meaningful,
# so 'date' is NOT here.
KEY_DATE_DATEONLY = {"payment_date"}

# Match-normalization classes (applied to KEYS and field COMPARISON only -- NOT to
# the value written, so stored case numbers/amounts keep their original form).
MONEY_COLS = {"payment_amount", "bond_amount"}     # '20' == '$20.00'
CASE_COLS = {"case_number"}                         # '@1641760, @1641763' == 'X@1641763 1641760'
LOWER_COLS = {"payment_type", "type_of_check_in"}   # 'allied' == 'Allied'
# Boolean/yes-no columns: blank == False == No, so SQL '' vs CSV 'False' isn't a
# change. Real flips (False<->True) still register.
BOOL_COLS = {"dma", "gps", "da_emailed", "ptr_successfully_completed",
             "victim_accept_deny_gps", "paid", "victim_time_48"}
_TRUTHY = {"true", "yes", "y", "1"}
_FALSY = {"false", "no", "n", "0", ""}

# Denormalized/redundant columns to ignore when comparing existing rows (still
# inserted for brand-new rows). raw_check_ins.defendant is blank in the live SQL and
# derivable from idn -- syncing it just floods the change log.
IGNORE_COMPARE = {"checkins": {"defendant"}}


def money_norm(s):
    t = re.sub(r"[\$,\s]", "", s or "")
    try:
        return f"{float(t):.2f}"
    except ValueError:
        return (s or "").strip()


def case_norm(s):
    toks = re.findall(r"\d+", s or "")
    return ",".join(sorted(toks)) if toks else (s or "").strip()


def bool_norm(s):
    t = (s or "").strip().lower()
    if t in _TRUTHY:
        return "true"
    if t in _FALSY:
        return "false"
    return t


def match_norm(col, s):
    """Extra normalization for matching/comparison so format noise in the live data
    (money symbols, case-number whitespace/order, type casing, booleans) doesn't
    break keys or flag cosmetic changes."""
    if col in MONEY_COLS:
        return money_norm(s)
    if col in CASE_COLS:
        return case_norm(s)
    if col in BOOL_COLS:
        return bool_norm(s)
    if col in LOWER_COLS:
        return (s or "").strip().lower()
    return s


def field_canon(v, *, dateonly=False):
    """canon() + date normalization. Used for the WRITTEN value and as the base for
    match_norm(). Converges stored dates to canonical Eastern form."""
    s = canon(v)
    d = canon_date(s, dateonly=dateonly)
    return d if d is not None else s


def _ensure_columns(conn, table, cols):
    have = {r[1] for r in conn.execute(f"PRAGMA table_info({table})")}
    for c in cols:
        if c not in have:
            conn.execute(f"ALTER TABLE {table} ADD COLUMN [{c}] NVARCHAR(500)")


def _ensure_log_table(conn):
    conn.execute("""
        CREATE TABLE IF NOT EXISTS import_change_log (
            id        INTEGER PRIMARY KEY AUTOINCREMENT,
            run_id    TEXT,
            ts        TEXT,
            dataset   TEXT,
            action    TEXT,          -- 'add' | 'change'
            row_key   TEXT,
            idn       TEXT,
            field     TEXT,          -- changed column ('*' for an add)
            old_value TEXT,
            new_value TEXT           -- new field value, or full-row JSON for an add
        )""")
    conn.execute("CREATE INDEX IF NOT EXISTS ix_icl_run ON import_change_log(run_id)")
    conn.execute("CREATE INDEX IF NOT EXISTS ix_icl_idn ON import_change_log(idn)")


def reconcile_dataset(conn, dataset, path, run_id, ts, logf, dry, allow_blanking=False,
                      adds_only=False):
    table = DATASETS[dataset][0]
    aliases = DATASETS[dataset][1]
    keycols = KEYS[dataset]
    headers, rows = _read_csv(path)
    if not headers:
        logf(f"{dataset}: EMPTY file, skipped (table unchanged)")
        return dict(added=0, changed=0, unchanged=0, csv_dups=0, sql_only=0, blanked=0, skipped=True)

    colmap = _match_headers(headers, aliases)        # db_col -> csv header
    # Drop sp_item_id: the Export-to-CSV files carry no SharePoint item ID, and
    # its alias "ID" greedily substring-matches the IDN header, so it would
    # otherwise be populated with the IDN value. Not a real, syncable field here.
    colmap.pop("sp_item_id", None)
    db_cols = list(colmap.keys())
    for kc in keycols:
        if kc not in colmap:
            logf(f"{dataset}: WARNING key column '{kc}' not in CSV; skipped. matched={db_cols}")
            return dict(added=0, changed=0, unchanged=0, csv_dups=0, sql_only=0, blanked=0, skipped=True)
    _ensure_columns(conn, table, db_cols)

    # Index existing SQL rows by natural key -> queue of (rowid, {col: val}).
    sql_index = defaultdict(list)
    sel_cols = sorted(set(db_cols) | set(keycols))
    for row in conn.execute(f"SELECT rowid,{','.join('['+c+']' for c in sel_cols)} FROM {table}"):
        rowid = row[0]
        d = {sel_cols[i]: row[i + 1] for i in range(len(sel_cols))}
        key = tuple(_cv(match_norm(k, field_canon(d.get(k), dateonly=(k in KEY_DATE_DATEONLY)))) for k in keycols)
        sql_index[key].append((rowid, d))
    matched_sql = set()

    cur = conn.cursor()
    added = changed = unchanged = csv_dups = blanked = 0
    insert_sql = (f"INSERT INTO {table} ({','.join('['+c+']' for c in db_cols)}) "
                  f"VALUES ({','.join('?' for _ in db_cols)})")

    def logrow(action, key, idn, field, old, new):
        if dry:
            return
        cur.execute(
            "INSERT INTO import_change_log "
            "(run_id,ts,dataset,action,row_key,idn,field,old_value,new_value) "
            "VALUES (?,?,?,?,?,?,?,?,?)",
            (run_id, ts, dataset, action, " | ".join(key), idn, field, old, new))

    seen_csv_keys = defaultdict(int)
    for r in rows:
        vals = {c: field_canon(r.get(colmap[c]), dateonly=(c in KEY_DATE_DATEONLY)) for c in db_cols}
        key = tuple(_cv(match_norm(k, vals.get(k))) for k in keycols)
        idn = vals.get("idn", "")
        queue = sql_index.get(key, [])
        # Consume one SQL row per CSV occurrence of this key (handles dup keys).
        occ = seen_csv_keys[key]
        seen_csv_keys[key] += 1
        if occ < len(queue):
            rowid, existing = queue[occ]
            matched_sql.add(rowid)
            if adds_only:
                # --adds-only: an existing row is left entirely untouched — no
                # field updates, no blank-keep churn. Only brand-new rows insert.
                unchanged += 1
                continue
            # Each diff: (col, old_display, write_value). Comparison is done on the
            # MATCH-NORMALIZED forms (so format noise isn't a change), but the value
            # WRITTEN is the plain field_canon value -- never the normalized form,
            # which would corrupt stored case numbers / amounts / type casing.
            ignore = IGNORE_COMPARE.get(dataset, ())
            diffs = []
            for c in db_cols:
                if c in ignore:
                    continue
                writeval = vals.get(c)
                oldraw = field_canon(existing.get(c), dateonly=(c in KEY_DATE_DATEONLY))
                newcmp = _cv(match_norm(c, writeval))
                oldcmp = _cv(match_norm(c, oldraw))
                if newcmp == oldcmp:
                    continue
                if _cv(writeval) == "" and _cv(oldraw) != "" and not allow_blanking:
                    # CSV omits a value SQL already has -> would blank a populated
                    # field. Treated as data loss; logged but NOT applied. (Checks the
                    # raw value, so a bool True->'' is caught even though bool_norm
                    # maps '' to 'false'.)
                    blanked += 1
                    logf(f"  KEPT   {dataset} idn={idn} key=[{' | '.join(key)}] "
                         f"{c}: kept {_cv(oldraw)!r} (CSV empty)", detail=True)
                    logrow("skip_blank", key, idn, c, _cv(oldraw), "")
                    continue
                diffs.append((c, _cv(oldraw), writeval))
            if diffs:
                changed += 1
                set_clause = ",".join(f"[{c}]=?" for c, _, _ in diffs)
                if not dry:
                    cur.execute(f"UPDATE {table} SET {set_clause} WHERE rowid=?",
                                [w for _, _, w in diffs] + [rowid])
                for c, old, w in diffs:
                    logf(f"  CHANGE {dataset} idn={idn} key=[{' | '.join(key)}] "
                         f"{c}: {old!r} -> {w!r}", detail=True)
                    logrow("change", key, idn, c, old, w)
            else:
                unchanged += 1
        elif occ > 0:
            # CSV repeats a key more times than SQL has rows: an unmatchable
            # duplicate (same in every stored field). Count, don't blind-insert.
            csv_dups += 1
            logf(f"  DUP    {dataset} idn={idn} key=[{' | '.join(key)}] "
                 f"(CSV has >1 identical row; kept 1)", detail=True)
        else:
            added += 1
            if not dry:
                cur.execute(insert_sql, [vals[c] for c in db_cols])
            logf(f"  ADD    {dataset} idn={idn} key=[{' | '.join(key)}]", detail=True)
            logrow("add", key, idn, "*", "", json.dumps(vals, ensure_ascii=False))

    sql_only = sum(len(q) for q in sql_index.values()) - len(matched_sql)
    logf(f"{dataset}: +{added} added, ~{changed} changed, ={unchanged} unchanged, "
         f"{blanked} blanks-kept, {csv_dups} csv-dups-skipped, {sql_only} in-SQL-not-in-CSV (kept)")
    return dict(added=added, changed=changed, unchanged=unchanged,
                csv_dups=csv_dups, sql_only=sql_only, blanked=blanked, skipped=False)


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


def build_report_message(recipients, subject, body, logpath, attach):
    """Assemble the EmailMessage. Body is the PII-free summary; the detail log
    (which contains IDNs/names) is attached only when attach=True."""
    from email.message import EmailMessage
    sender = os.environ.get("REPORT_FROM") or os.environ.get("SMTP_USER") or os.environ.get("IMAP_USER", "")
    msg = EmailMessage()
    msg["From"] = sender
    msg["To"] = ", ".join(recipients)
    msg["Subject"] = subject
    msg.set_content(body)
    if attach and logpath and Path(logpath).exists():
        data = Path(logpath).read_bytes()
        msg.add_attachment(data, maintype="text", subtype="plain",
                           filename=Path(logpath).name)
    return msg


def send_report(recipients, subject, body, logpath, attach):
    import smtplib
    import ssl
    imap_host = os.environ.get("IMAP_HOST", "")
    host = (os.environ.get("SMTP_HOST")
            or (imap_host.replace("imap", "smtp") if imap_host else "")
            or "smtp.gmail.com")
    port = int(os.environ.get("SMTP_PORT", "587"))
    user = os.environ.get("SMTP_USER") or os.environ.get("IMAP_USER")
    pw = os.environ.get("SMTP_PASS") or os.environ.get("IMAP_PASS")
    if not (user and pw):
        raise RuntimeError("no SMTP_USER/PASS (or IMAP_USER/PASS) configured")
    msg = build_report_message(recipients, subject, body, logpath, attach)
    ctx = ssl.create_default_context()
    if port == 465:
        with smtplib.SMTP_SSL(host, port, context=ctx, timeout=30) as s:
            s.login(user, pw); s.send_message(msg)
    else:
        with smtplib.SMTP(host, port, timeout=30) as s:
            s.starttls(context=ctx); s.login(user, pw); s.send_message(msg)


def send_ntfy(url, title, message):
    """POST a short notification to an ntfy topic (no PII -- counts/notes only).
    url is the full topic URL, e.g. https://ntfy.sh/ptr-alerts-xxxx."""
    req = urllib.request.Request(
        url, data=message.encode("utf-8"), method="POST",
        headers={"Title": title, "Tags": "arrows_counterclockwise",
                 "Priority": "default"})
    with urllib.request.urlopen(req, timeout=20) as resp:
        resp.read()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", required=True, help="folder containing the 4 CSV exports")
    ap.add_argument("--db", default=os.environ.get("SQLITE_DB_PATH", "/opt/ptr-knoxc/db/kh222.db"))
    ap.add_argument("--dry-run", action="store_true", help="report changes, write nothing")
    ap.add_argument("--log-dir", default=None, help="where to write the text log (default <db_dir>/import_logs)")
    ap.add_argument("--allow-blanking", action="store_true",
                    help="let an empty CSV value overwrite a populated SQL field "
                         "(default OFF: empty CSV never blanks existing data)")
    ap.add_argument("--adds-only", action="store_true",
                    help="only INSERT rows missing from SQL; existing rows are left "
                         "entirely untouched (no field updates)")
    ap.add_argument("--summary-json", default=None,
                    help="also write a machine-readable run summary (counts, run_id, "
                         "log path) to this JSON file — used by the web upload page")
    ap.add_argument("--no-email", action="store_true",
                    help="never send the emailed report, even if REPORT_TO is configured")
    ap.add_argument("--stamp-meta", default=None, metavar="MODE",
                    help="on a committed (non-dry) run, stamp import_meta last_import/"
                         "last_import_mode=MODE like the daily importer, so the console's "
                         "'Data refreshed' footer reflects this sync")
    ap.add_argument("--email", action="store_true",
                    help="email the run summary (recipients from --email-to or REPORT_TO)")
    ap.add_argument("--email-to", default=None, help="comma-separated recipients (implies --email)")
    ap.add_argument("--attach-log", action="store_true",
                    help="attach the full detail log to the email (contains IDNs/names)")
    ap.add_argument("--ntfy", action="store_true",
                    help="push a short summary to ntfy (url from --ntfy-url or NTFY_URL)")
    ap.add_argument("--ntfy-url", default=None, help="ntfy topic URL (implies --ntfy)")
    ap.add_argument("--note", default=None,
                    help="free-text note prepended to the email + ntfy (e.g. 'manual sync, current as of 1358')")
    args = ap.parse_args()

    db = Path(args.db)
    if not db.exists():
        log(f"FATAL: DB not found at {db}"); return 2
    csvs = find_csvs_from_dir(args.dir)
    missing = [d for d in ORDER if d not in csvs]
    if missing:
        log(f"FATAL: missing CSV(s) for: {missing}. found={sorted(csvs)}"); return 3

    now = datetime.now(timezone.utc)
    ts = now.isoformat(timespec="seconds")
    run_id = now.strftime("%Y%m%dT%H%M%SZ")
    log_dir = Path(args.log_dir) if args.log_dir else db.parent / "import_logs"
    log_dir.mkdir(parents=True, exist_ok=True)
    logpath = log_dir / f"reconcile-{run_id}.log"

    detail_lines = []
    summary_lines = []

    def logf(msg, detail=False):
        line = msg
        (detail_lines if detail else summary_lines).append(line)
        log(line)

    logf(f"=== reconcile run {run_id} {'(DRY RUN)' if args.dry_run else ''} ===")
    if args.note:
        logf(f"note: {args.note}")
    logf(f"db={db}  dir={args.dir}")

    conn = sqlite3.connect(str(db), timeout=30)
    conn.execute("PRAGMA journal_mode=WAL"); conn.execute("PRAGMA busy_timeout=30000")
    totals = defaultdict(int)
    per_dataset = {}
    try:
        conn.execute("BEGIN")
        if not args.dry_run:
            _ensure_log_table(conn)
        for ds in ORDER:
            s = reconcile_dataset(conn, ds, csvs[ds], run_id, ts, logf,
                                  args.dry_run, args.allow_blanking, args.adds_only)
            per_dataset[ds] = s
            for k, v in s.items():
                if k == "skipped":  # per-dataset flag, not a summable count
                    continue
                totals[k] += v
        logf(f"TOTAL: +{totals['added']} added, ~{totals['changed']} changed, "
             f"={totals['unchanged']} unchanged, {totals['blanked']} blanks-kept, "
             f"{totals['csv_dups']} csv-dups, {totals['sql_only']} kept-not-in-csv")
        # Only stamp the freshness clock if the run actually reconciled at least
        # one dataset. A run where every file was skipped (wrong slots / empty
        # exports) reconciled nothing, so it must NOT reset the "data updated"
        # staleness indicator — that would mask a stale or broken pipeline.
        reconciled_any = any(not per_dataset[d].get("skipped") for d in ORDER)
        if args.stamp_meta and not args.dry_run and reconciled_any:
            # Freshness stamp, committed atomically with the data — same rows the
            # daily importer writes, so the console footer counts this as a refresh.
            conn.execute("CREATE TABLE IF NOT EXISTS import_meta (key TEXT PRIMARY KEY, value TEXT)")
            now_utc = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
            for k, v in (("last_import", now_utc), ("last_import_mode", args.stamp_meta)):
                conn.execute(
                    "INSERT INTO import_meta(key, value) VALUES(?, ?) "
                    "ON CONFLICT(key) DO UPDATE SET value = excluded.value", (k, v))
            logf(f"stamped import_meta (mode={args.stamp_meta})")
        elif args.stamp_meta and not args.dry_run:
            logf("NOT stamping import_meta: every dataset was skipped (no data reconciled)")
        if args.dry_run:
            conn.execute("ROLLBACK"); logf("dry-run: rolled back, no changes written")
        else:
            conn.commit(); logf(f"commit OK; change log run_id={run_id} in import_change_log")
    except Exception as e:
        conn.execute("ROLLBACK"); log(f"ERROR: rolled back: {e}"); return 1
    finally:
        conn.close()

    # Post-commit bookkeeping. The DB work is already committed (or rolled back on
    # a dry run); a failure writing these files must NOT flip the exit code, or the
    # web caller would report a committed import as failed. Write the machine-
    # readable summary FIRST — that's the signal the upload page reads (its absence
    # after a run means the run died before/at commit).
    if args.summary_json:
        try:
            Path(args.summary_json).write_text(json.dumps({
                "run_id": run_id, "dry_run": args.dry_run, "adds_only": args.adds_only,
                "datasets": per_dataset, "totals": dict(totals),
                "log_path": str(logpath), "ok": True,
            }, indent=1), encoding="utf-8")
            log(f"wrote summary json: {args.summary_json}")
        except Exception as e:
            log(f"WARNING: summary json write failed (data already committed): {e}")

    # Human-readable file log (summary first, then detail).
    body = "\n".join(summary_lines) + "\n\n--- detail ---\n" + "\n".join(detail_lines) + "\n"
    try:
        logpath.write_text(body, encoding="utf-8")
        log(f"wrote text log: {logpath}")
    except Exception as e:
        log(f"WARNING: text log write failed (data already committed): {e}")

    # Optional emailed report: PII-free summary in the body; full log attached
    # only with --attach-log. Sent AFTER the DB work, so a mail failure never
    # affects the committed reconcile.
    recipients = [a.strip() for a in (args.email_to or os.environ.get("REPORT_TO") or DEFAULT_REPORT_TO).split(",") if a.strip()]
    if args.no_email:
        recipients = []
    if args.email or args.email_to or recipients:
        if not recipients:
            log("WARNING: email requested but no recipients (set --email-to or REPORT_TO); skipped")
        else:
            subject = (f"PTR reconcile {run_id}{' [DRY RUN]' if args.dry_run else ''}: "
                       f"+{totals['added']} added, ~{totals['changed']} changed, "
                       f"{totals['blanked']} blanks-kept")
            try:
                send_report(recipients, subject, "\n".join(summary_lines), logpath, args.attach_log)
                log(f"emailed report to {', '.join(recipients)}"
                    + (" (with detail log)" if args.attach_log else " (summary only)"))
            except Exception as e:
                log(f"WARNING: email failed (reconcile already committed): {e}")

    # Optional ntfy push: short PII-free line (note + headline counts). Also
    # post-commit; failure only warns.
    ntfy_url = args.ntfy_url or os.environ.get("NTFY_URL", "")
    if args.ntfy or args.ntfy_url or (args.note and ntfy_url):
        if not ntfy_url:
            log("WARNING: ntfy requested but no URL (set --ntfy-url or NTFY_URL); skipped")
        else:
            title = "PTR manual sync" + (" [DRY RUN]" if args.dry_run else "")
            msg = ((args.note + "\n") if args.note else "") + (
                f"+{totals['added']} added, ~{totals['changed']} changed, "
                f"{totals['unchanged']} unchanged, {totals['blanked']} blanks-kept")
            try:
                send_ntfy(ntfy_url, title, msg)
                log("pushed ntfy notification")
            except Exception as e:
                log(f"WARNING: ntfy failed (reconcile already committed): {e}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
