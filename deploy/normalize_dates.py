#!/usr/bin/env python3
"""normalize_dates.py — one-time fix for the PT1 "dates one day off" bug.

ROOT CAUSE: referral / payment / check-in dates were stored as UTC 'Z' timestamps
(e.g. '2025-12-11T00:00:00Z', which is Dec 10 7pm Eastern). The Go website reads the
literal date part of the string -> shows Dec 11, one day AHEAD of the true Eastern
date (Dec 10) that the SharePoint export and the offline tracker show. Date-only
columns (gps_install_date, closed_date) are unaffected.

THE FIX: rewrite each affected value to its canonical Eastern form using the SAME
DST-correct UTC->Eastern logic the reconcile importer uses (canon_date, copied
verbatim below). Only rows whose DISPLAYED day actually changes are touched, so the
change set is exactly the off-by-one rows. Because the new value equals its own
canonical form, a later reconcile sees no difference -> no churn, no duplicate rows.

Per-column target (matches what reconcile_import.py converges to):
  raw_blue_book.referral_date -> Eastern date-only        (YYYY-MM-DD)
  raw_payments.payment_date   -> Eastern date-only        (YYYY-MM-DD; keyed date-only)
  raw_check_ins.date          -> Eastern, time preserved  (YYYY-MM-DD HH:MM; time is keyed)

SAFE: timestamped backup copy first, single transaction, text audit log, integrity
check, idempotent (2nd run = 0 changes). Dry-run by default; pass --apply to commit.

Usage:
  python3 normalize_dates.py --db /opt/ptr-knoxc/db/kh222.db           # preview
  python3 normalize_dates.py --db /opt/ptr-knoxc/db/kh222.db --apply   # commit
"""
import argparse
import os
import re
import shutil
import sqlite3
import sys
from datetime import datetime, timedelta

# ── canon_date + helpers: copied VERBATIM from webapp/reconcile_import.py so this
#    migration produces byte-identical output to the importer (no divergence). ──
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
    """Naive UTC -> naive US Eastern, DST-correct, no tzdata dep (EDT -4 from 2nd
    Sun Mar 02:00 local to 1st Sun Nov 02:00, else EST -5)."""
    y = dt.year
    edt_start = datetime(y, 3, _nth_sunday(y, 3, 2), 7)    # 02:00 EST == 07:00 UTC
    edt_end = datetime(y, 11, _nth_sunday(y, 11, 1), 6)    # 02:00 EDT == 06:00 UTC
    off = -4 if (edt_start <= dt < edt_end) else -5
    return dt + timedelta(hours=off)


def canon_date(s, dateonly=False):
    """Parse a value as a date/datetime -> canonical 'YYYY-MM-DD[ HH:MM]', or None.
    UTC 'Z' timestamps are converted to Eastern; dateonly drops the time."""
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
        if is_utc and has_time:
            dt = _utc_to_eastern(dt)
        if dateonly:
            return dt.strftime("%Y-%m-%d")
        if dt.hour or dt.minute:
            return dt.strftime("%Y-%m-%d %H:%M")
        return dt.strftime("%Y-%m-%d")
    return None


# ── how the Go app currently extracts the displayed day (compute.ParseDay): take
#    the leading ISO date, else a US M/D/Y — mirrors reISO/reUS in compute.go. ──
_RE_ISO = re.compile(r"^(\d{4})-(\d{1,2})-(\d{1,2})")
_RE_US = re.compile(r"^(\d{1,2})/(\d{1,2})/(\d{4})")


def displayed_day(s):
    """The YYYY-MM-DD the Go site currently shows for a stored value, or None."""
    s = (s or "").strip()
    m = _RE_ISO.match(s)
    if m:
        return "%04d-%02d-%02d" % (int(m.group(1)), int(m.group(2)), int(m.group(3)))
    m = _RE_US.match(s)
    if m:
        return "%04d-%02d-%02d" % (int(m.group(3)), int(m.group(1)), int(m.group(2)))
    return None


# (table, column, dateonly) — dateonly matches the importer's per-column treatment.
COLUMNS = [
    ("raw_blue_book", "referral_date", True),
    ("raw_payments", "payment_date", True),
    ("raw_check_ins", "date", False),
]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", required=True, help="path to the SQLite DB")
    ap.add_argument("--apply", action="store_true", help="commit (default: dry-run)")
    args = ap.parse_args()

    if not os.path.exists(args.db):
        print("ERROR: no such DB:", args.db)
        sys.exit(1)

    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    log_path = os.path.join(os.path.dirname(os.path.abspath(args.db)),
                            "normalize_dates-%s.log" % stamp)

    if args.apply:
        backup = "%s.bak-%s" % (args.db, stamp)
        shutil.copy2(args.db, backup)
        print("backup ->", backup)

    con = sqlite3.connect(args.db)
    con.isolation_level = None
    cur = con.cursor()
    cur.execute("BEGIN")

    logf = open(log_path, "w", encoding="utf-8")
    logf.write("normalize_dates %s  db=%s  apply=%s\n\n" % (stamp, args.db, args.apply))

    grand_fixed = 0
    grand_seen = 0
    for table, col, dateonly in COLUMNS:
        rows = cur.execute(
            'SELECT rowid, "%s" FROM "%s" WHERE "%s" IS NOT NULL AND TRIM("%s") <> \'\''
            % (col, table, col, col)
        ).fetchall()
        fixed = 0
        for rowid, old in rows:
            grand_seen += 1
            new = canon_date(old, dateonly=dateonly)
            if not new:
                continue
            old_day = displayed_day(old)
            new_day = displayed_day(new)
            if old_day and new_day and old_day != new_day:
                # the displayed day actually changes -> this is an off-by-one row
                cur.execute('UPDATE "%s" SET "%s"=? WHERE rowid=?' % (table, col),
                            (new, rowid))
                logf.write("%-22s rowid=%-7d %r -> %r  (%s -> %s)\n"
                           % (table + "." + col, rowid, old, new, old_day, new_day))
                fixed += 1
        print("  %-30s scanned %5d  fixed %5d" % (table + "." + col, len(rows), fixed))
        logf.write("\n# %s.%s scanned=%d fixed=%d\n\n" % (table, col, len(rows), fixed))
        grand_fixed += fixed

    integrity = cur.execute("PRAGMA integrity_check").fetchone()[0]
    print("integrity_check:", integrity)
    logf.write("integrity_check: %s\n" % integrity)

    if args.apply and integrity == "ok":
        cur.execute("COMMIT")
        print("COMMITTED. fixed %d rows total." % grand_fixed)
    else:
        cur.execute("ROLLBACK")
        if args.apply:
            print("ROLLED BACK (integrity != ok).")
        else:
            print("DRY-RUN (no changes written). would fix %d rows." % grand_fixed)
    logf.close()
    con.close()
    print("log ->", log_path)


if __name__ == "__main__":
    main()
