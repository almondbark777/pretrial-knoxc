"""
update_db.py
------------
Surgical update of kh222.db from new CSV exports.  Unlike build_db.py (which
rebuilds from scratch), this preserves:
  * raw_master_list (master list xlsx not exported here)
  * the migration tables (defendant_notes, violations, user_preferences, etc.)
  * any session-stored state on those migration tables

Replaces:
  * raw_blue_book, raw_payments, raw_check_ins, raw_gps_48_hours
  * normalized: defendants, cases, payments, check_ins, gps_events

CSVs from this export do NOT contain a SharePoint ListSchema blob row 0 — the
first row is the actual header.

Usage:
    python db/update_db.py --uploads "C:/Users/alexa/Downloads"
"""
from __future__ import annotations

import argparse
import csv
import re
import sqlite3
import sys
from datetime import datetime
from pathlib import Path


def snake(name: str) -> str:
    if name is None:
        return ""
    s = str(name).strip().replace("%23", "num")
    s = re.sub(r"[^\w]+", "_", s).strip("_")
    s = re.sub(r"_+", "_", s)
    return s.lower()


def dedupe_columns(cols):
    seen, out = {}, []
    for c in cols:
        if c in seen:
            seen[c] += 1
            out.append(f"{c}_{seen[c]}")
        else:
            seen[c] = 1
            out.append(c)
    return out


_DATE_FORMATS = (
    "%m/%d/%Y %H:%M",
    "%m/%d/%Y",
    "%Y-%m-%d %H:%M:%S",
    "%Y-%m-%d",
)


def parse_date(v):
    if v is None or v == "":
        return None
    if isinstance(v, datetime):
        return v.strftime("%Y-%m-%dT%H:%M:%S")
    s = str(v).strip()
    if not s:
        return None
    try:
        if s.endswith("Z"):
            return datetime.strptime(s[:-1], "%Y-%m-%dT%H:%M:%S").strftime("%Y-%m-%dT%H:%M:%S")
        if "T" in s:
            return datetime.fromisoformat(s).strftime("%Y-%m-%dT%H:%M:%S")
    except Exception:
        pass
    for fmt in _DATE_FORMATS:
        try:
            return datetime.strptime(s, fmt).strftime("%Y-%m-%dT%H:%M:%S")
        except Exception:
            continue
    return s


_MONEY_RE = re.compile(r"[^\d\.\-]")


def parse_money(v):
    if v is None or v == "":
        return None
    if isinstance(v, (int, float)):
        return float(v)
    s = _MONEY_RE.sub("", str(v))
    if not s or s in {".", "-"}:
        return None
    try:
        return float(s)
    except ValueError:
        return None


def parse_bool(v):
    if v is None or v == "":
        return None
    if isinstance(v, bool):
        return int(v)
    s = str(v).strip().lower()
    if s in {"true", "yes", "1", "y", "t"}:
        return 1
    if s in {"false", "no", "0", "n", "f"}:
        return 0
    return None


def parse_int(v):
    if v is None or v == "":
        return None
    if isinstance(v, int):
        return v
    try:
        return int(float(str(v).replace(",", "")))
    except ValueError:
        return None


def split_cases(raw):
    if raw is None:
        return []
    s = str(raw).strip()
    if not s:
        return []
    out = []
    for piece in re.split(r"[,;&\s]+", s):
        p = piece.strip().lstrip("@").strip()
        if p and p not in out:
            out.append(p)
    return out


def read_plain_csv(path: Path):
    """First row is the actual header (no SharePoint ListSchema blob)."""
    with path.open("r", encoding="utf-8-sig", newline="") as fh:
        rows = list(csv.reader(fh))
    if not rows:
        return [], []
    header = dedupe_columns([snake(h) for h in rows[0]])
    return header, rows[1:]


def replace_raw_table(con, name, header, data):
    con.execute(f'DROP TABLE IF EXISTS "{name}"')
    cols_sql = ",\n    ".join(f'"{h}" TEXT' for h in header)
    con.execute(f'CREATE TABLE "{name}" (\n    {cols_sql}\n)')
    if data:
        ph = ",".join("?" * len(header))
        qcols = ",".join(f'"{h}"' for h in header)
        con.executemany(
            f'INSERT INTO "{name}" ({qcols}) VALUES ({ph})',
            data,
        )


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--uploads", required=True, help="Folder with the 4 CSV files")
    ap.add_argument("--db", default="db/kh222.db", help="Path to kh222.db (default: db/kh222.db)")
    args = ap.parse_args()

    up = Path(args.uploads).resolve()
    db_path = Path(args.db).resolve()

    csv_sources = {
        "raw_blue_book":    up / "New Blue Book.csv",
        "raw_payments":     up / "Payments.csv",
        "raw_check_ins":    up / "Check Ins.csv",
        "raw_gps_48_hours": up / "GPS 48 Hours.csv",
    }

    for tbl, p in csv_sources.items():
        if not p.exists():
            sys.exit(f"missing source: {p}")

    con = sqlite3.connect(db_path)
    con.execute("PRAGMA foreign_keys = OFF")

    # ---- replace raw_* tables ----
    headers = {}
    for tbl, path in csv_sources.items():
        h, d = read_plain_csv(path)
        headers[tbl] = h
        replace_raw_table(con, tbl, h, d)
        print(f"  {tbl}: {len(d)} rows")

    # ---- truncate normalized tables ----
    for t in ("cases", "payments", "check_ins", "gps_events"):
        con.execute(f'DELETE FROM "{t}"')
    con.execute('DELETE FROM defendants')

    # ---- defendants: merge raw_blue_book + raw_master_list ----
    bb_idx = {h: i for i, h in enumerate(headers["raw_blue_book"])}

    def bb(row, key):
        return row[bb_idx[key]] if key in bb_idx else None

    bb_rows = list(con.execute(
        f'SELECT {",".join(chr(34) + h + chr(34) for h in headers["raw_blue_book"])} FROM raw_blue_book'
    ))

    defendants: dict[int, dict] = {}
    for row in bb_rows:
        idn = parse_int(bb(row, "idn"))
        if idn is None:
            continue
        defendants[idn] = dict(
            idn=idn,
            defendant_name=bb(row, "defendant"),
            defendant_last_name=None,
            birthdate=parse_date(bb(row, "birthdate")),
            pretrial_level=parse_int(bb(row, "pretrial_level")),
            charge_type=bb(row, "charge_type"),
            supervision_type=bb(row, "supervision_type"),
            order_from=bb(row, "order_from"),
            dma=parse_bool(bb(row, "dma")),
            gps=parse_bool(bb(row, "gps")),
            referral_date=parse_date(bb(row, "referral_date")),
            closed_date=parse_date(bb(row, "closed_date")),
            case_status=bb(row, "case_status"),
            supervising_officer=bb(row, "supervising_officer"),
            ptr_successfully_completed=parse_bool(bb(row, "ptr_successfully_completed")),
            bond_amount=parse_money(bb(row, "bond_amount")),
            total_paid=parse_money(bb(row, "total_paid")),
            source="blue_book",
        )

    ml_cols = [r[1] for r in con.execute("PRAGMA table_info(raw_master_list)")]
    ml_idx = {h: i for i, h in enumerate(ml_cols)}
    ml_rows = list(con.execute(
        f'SELECT {",".join(chr(34) + c + chr(34) for c in ml_cols)} FROM raw_master_list'
    ))
    for row in ml_rows:
        idn = parse_int(row[ml_idx["idn"]] if "idn" in ml_idx else None)
        if idn is None:
            continue
        last = row[ml_idx["defendant_last_name"]] if "defendant_last_name" in ml_idx else None
        if idn in defendants:
            d = defendants[idn]
            d["source"] = "both"
            if not d.get("defendant_last_name"):
                d["defendant_last_name"] = last
        else:
            defendants[idn] = dict(
                idn=idn, defendant_name=None, defendant_last_name=last,
                birthdate=None,
                pretrial_level=parse_int(row[ml_idx["pretrial_level"]]) if "pretrial_level" in ml_idx else None,
                charge_type=row[ml_idx["charge_type"]] if "charge_type" in ml_idx else None,
                supervision_type=row[ml_idx["supervision_type"]] if "supervision_type" in ml_idx else None,
                order_from=row[ml_idx["order_from"]] if "order_from" in ml_idx else None,
                dma=parse_bool(row[ml_idx["dma"]]) if "dma" in ml_idx else None,
                gps=None,
                referral_date=parse_date(row[ml_idx["referral_date"]]) if "referral_date" in ml_idx else None,
                closed_date=None,
                case_status=None, supervising_officer=None,
                ptr_successfully_completed=None,
                bond_amount=None, total_paid=None,
                source="master_list",
            )

    cols = ["idn","defendant_name","defendant_last_name","birthdate","pretrial_level",
            "charge_type","supervision_type","order_from","dma","gps","referral_date",
            "closed_date","case_status","supervising_officer","ptr_successfully_completed",
            "bond_amount","total_paid","source"]
    con.executemany(
        f"INSERT INTO defendants ({','.join(cols)}) VALUES ({','.join('?'*len(cols))})",
        [tuple(d.get(c) for c in cols) for d in defendants.values()],
    )

    # ---- cases ----
    case_pairs: set[tuple[int, str]] = set()
    for row in bb_rows:
        idn = parse_int(bb(row, "idn"))
        raw = bb(row, "warrant_case_num")
        if idn is None:
            continue
        for cn in split_cases(raw):
            case_pairs.add((idn, cn))

    pay_cols = headers["raw_payments"]
    pay_idx = {h: i for i, h in enumerate(pay_cols)}
    pay_rows = list(con.execute(
        f'SELECT {",".join(chr(34) + c + chr(34) for c in pay_cols)} FROM raw_payments'
    ))
    for row in pay_rows:
        idn = parse_int(row[pay_idx["idn"]]) if "idn" in pay_idx else None
        raw = row[pay_idx["case_number"]] if "case_number" in pay_idx else None
        if idn is None:
            continue
        for cn in split_cases(raw):
            case_pairs.add((idn, cn))

    con.executemany(
        "INSERT INTO cases (idn, case_number, source) VALUES (?, ?, ?)",
        [(i, c, "derived") for (i, c) in sorted(case_pairs)],
    )

    # ---- payments ----
    con.executemany(
        "INSERT INTO payments (idn, case_number, defendant, payment_date, "
        "payment_amount, officer, payment_type, case_status) "
        "VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
        [(
            parse_int(r[pay_idx["idn"]]) if "idn" in pay_idx else None,
            r[pay_idx["case_number"]] if "case_number" in pay_idx else None,
            r[pay_idx["defendant"]] if "defendant" in pay_idx else None,
            parse_date(r[pay_idx["payment_date"]]) if "payment_date" in pay_idx else None,
            parse_money(r[pay_idx["payment_amount"]]) if "payment_amount" in pay_idx else None,
            r[pay_idx.get("officer_that_collected_payment", pay_idx.get("officer", -1))]
                if ("officer_that_collected_payment" in pay_idx or "officer" in pay_idx) else None,
            r[pay_idx["payment_type"]] if "payment_type" in pay_idx else None,
            r[pay_idx["case_status"]] if "case_status" in pay_idx else None,
        ) for r in pay_rows],
    )

    # ---- check_ins ----
    ci_cols = headers["raw_check_ins"]
    ci_idx = {h: i for i, h in enumerate(ci_cols)}
    ci_rows = list(con.execute(
        f'SELECT {",".join(chr(34) + c + chr(34) for c in ci_cols)} FROM raw_check_ins'
    ))
    con.executemany(
        "INSERT INTO check_ins (idn, case_number, defendant, check_in_date, "
        "type_of_check_in, supervising_officer, case_status, referral_date, pretrial_level) "
        "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
        [(
            parse_int(r[ci_idx["idn"]]) if "idn" in ci_idx else None,
            r[ci_idx["case_number"]] if "case_number" in ci_idx else None,
            r[ci_idx["defendant"]] if "defendant" in ci_idx else None,
            parse_date(r[ci_idx.get("date", ci_idx.get("check_in_date", -1))])
                if ("date" in ci_idx or "check_in_date" in ci_idx) else None,
            r[ci_idx["type_of_check_in"]] if "type_of_check_in" in ci_idx else None,
            r[ci_idx["supervising_officer"]] if "supervising_officer" in ci_idx else None,
            r[ci_idx["case_status"]] if "case_status" in ci_idx else None,
            parse_date(r[ci_idx["referral_date"]]) if "referral_date" in ci_idx else None,
            parse_int(r[ci_idx["pretrial_level"]]) if "pretrial_level" in ci_idx else None,
        ) for r in ci_rows],
    )

    # ---- gps_events ----
    gp_cols = headers["raw_gps_48_hours"]
    gp_idx = {h: i for i, h in enumerate(gp_cols)}
    gp_rows = list(con.execute(
        f'SELECT {",".join(chr(34) + c + chr(34) for c in gp_cols)} FROM raw_gps_48_hours'
    ))
    con.executemany(
        "INSERT INTO gps_events (idn, case_number, defendant, referral_date, gps_type, "
        "case_status, paid, victim, victim_idn, victim_time_48, victim_accept_deny_gps, "
        "gps_install_date, court_order, da_emailed, closed_date) "
        "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
        [(
            parse_int(r[gp_idx["idn"]]) if "idn" in gp_idx else None,
            r[gp_idx["case_number"]] if "case_number" in gp_idx else None,
            r[gp_idx["defendant"]] if "defendant" in gp_idx else None,
            parse_date(r[gp_idx["referral_date"]]) if "referral_date" in gp_idx else None,
            r[gp_idx["gps_type"]] if "gps_type" in gp_idx else None,
            r[gp_idx["case_status"]] if "case_status" in gp_idx else None,
            parse_bool(r[gp_idx["paid"]]) if "paid" in gp_idx else None,
            r[gp_idx["victim"]] if "victim" in gp_idx else None,
            parse_int(r[gp_idx["victim_idn"]]) if "victim_idn" in gp_idx else None,
            parse_date(r[gp_idx["victim_time_48"]]) if "victim_time_48" in gp_idx else None,
            parse_bool(r[gp_idx["victim_accept_deny_gps"]]) if "victim_accept_deny_gps" in gp_idx else None,
            parse_date(r[gp_idx["gps_install_date"]]) if "gps_install_date" in gp_idx else None,
            r[gp_idx.get("order", gp_idx.get("court_order", -1))]
                if ("order" in gp_idx or "court_order" in gp_idx) else None,
            parse_bool(r[gp_idx["da_emailed"]]) if "da_emailed" in gp_idx else None,
            parse_date(r[gp_idx["closed_date"]]) if "closed_date" in gp_idx else None,
        ) for r in gp_rows],
    )

    con.commit()
    con.execute("PRAGMA foreign_keys = ON")

    # ---- summary ----
    print("\nFinal counts:")
    for t in ("raw_blue_book", "raw_payments", "raw_check_ins", "raw_gps_48_hours",
              "defendants", "cases", "payments", "check_ins", "gps_events"):
        n = con.execute(f"SELECT COUNT(*) FROM {t}").fetchone()[0]
        print(f"  {t}: {n}")

    con.close()


if __name__ == "__main__":
    main()
