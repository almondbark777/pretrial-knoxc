"""
queries.py — SQL queries for the Knox County Pre-Trial Services web app.

All queries return Python dicts/lists ready for JSON serialization.
The database is a local SQLite file (db/kh222.db) kept current by the
SharePoint import timer (sharepoint_import.py / ptr-import.timer).
"""
from __future__ import annotations
import os
import re
import time
import threading
from decimal import Decimal
from datetime import datetime
from pathlib import Path

from sqlite_compat import connect as _sconnect, Connection

# ─── Connection pool (just a single re-used connection, good enough here) ───

_lock = threading.Lock()
_conn = None  # type: ignore[assignment]


def _connect() -> Connection:
    """Open a fresh SQLite connection to db/kh222.db (or SQLITE_DB_PATH)."""
    path = os.environ.get("SQLITE_DB_PATH") or str(
        Path(__file__).parent.parent / "db" / "kh222.db"
    )
    return _sconnect(path)


def get_conn() -> Connection:
    global _conn
    with _lock:
        if _conn is None:
            _conn = _connect()
        else:
            try:
                cur = _conn.cursor(); cur.execute("SELECT 1"); cur.fetchone()
            except Exception:
                try: _conn.close()
                except Exception: pass
                _conn = _connect()
        return _conn


# ─── Tiny TTL cache ─────────────────────────────────────────────────────────

_cache: dict[str, tuple[float, object]] = {}


def cached(key: str, ttl_s: float, fn):
    now = time.time()
    hit = _cache.get(key)
    if hit and hit[0] > now:
        return hit[1]
    val = fn()
    _cache[key] = (now + ttl_s, val)
    return val


# ─── Helpers ────────────────────────────────────────────────────────────────

def _fmt_officer(email: str | None) -> str | None:
    """Nicholas.Loveless@knoxsheriff.org → Nicholas Loveless"""
    if not email:
        return None
    local = email.split("@", 1)[0]
    return " ".join(p.capitalize() for p in re.split(r"[._]+", local) if p)


def _fmt_date(v) -> str | None:
    """Normalize any stored date-ish string to MM/DD/YYYY."""
    if not v:
        return None
    if isinstance(v, datetime):
        return v.strftime("%m/%d/%Y")
    s = str(v).strip()
    for fmt in ("%Y-%m-%dT%H:%M:%S", "%Y-%m-%d %H:%M:%S", "%Y-%m-%d",
                "%m/%d/%Y %H:%M", "%m/%d/%Y"):
        try:
            return datetime.strptime(s, fmt).strftime("%m/%d/%Y")
        except ValueError:
            continue
    # ISO with timezone
    try:
        return datetime.fromisoformat(s.replace("Z", "+00:00")).strftime("%m/%d/%Y")
    except Exception:
        return s


def _d(v) -> float:
    if v is None: return 0.0
    if isinstance(v, Decimal): return float(v)
    try: return float(v)
    except: return 0.0


# ─── Queries ────────────────────────────────────────────────────────────────

def dashboard_stats() -> dict:
    """Stats scoped to the current calendar month (so far)."""
    c = get_conn().cursor()
    today = datetime.utcnow()
    month_label = today.strftime("%B %Y")

    # All date columns are NVARCHAR(50); use TRY_CONVERT and DATEFROMPARTS for the
    # month boundary so we can compare on the server.
    month_start_sql = "DATEFROMPARTS(YEAR(GETDATE()), MONTH(GETDATE()), 1)"
    next_month_sql  = "DATEADD(month, 1, " + month_start_sql + ")"

    c.execute(f"""
        SELECT COUNT(*) FROM defendants
        WHERE source IN ('blue_book','both')
          AND TRY_CONVERT(datetime2, referral_date) >= {month_start_sql}
          AND TRY_CONVERT(datetime2, referral_date) <  {next_month_sql}
    """)
    new_referrals = c.fetchone()[0]

    c.execute(f"""
        SELECT COUNT(*) FROM gps_events
        WHERE TRY_CONVERT(datetime2, gps_install_date) >= {month_start_sql}
          AND TRY_CONVERT(datetime2, gps_install_date) <  {next_month_sql}
    """)
    gps_installs = c.fetchone()[0]

    c.execute(f"""
        SELECT COUNT(*) FROM check_ins
        WHERE TRY_CONVERT(datetime2, check_in_date) >= {month_start_sql}
          AND TRY_CONVERT(datetime2, check_in_date) <  {next_month_sql}
    """)
    ci_month = c.fetchone()[0]

    c.execute(f"""
        SELECT ISNULL(SUM(payment_amount),0) FROM payments
        WHERE TRY_CONVERT(datetime2, payment_date) >= {month_start_sql}
          AND TRY_CONVERT(datetime2, payment_date) <  {next_month_sql}
    """)
    fees_month = _d(c.fetchone()[0])

    return {
        "month_label":          month_label,
        "new_referrals_month":  new_referrals,
        "gps_installs_month":   gps_installs,
        "check_ins_month":      ci_month,
        "fees_collected_month": fees_month,
    }


def officer_caseloads() -> list[dict]:
    c = get_conn().cursor()
    c.execute("""
        SELECT supervising_officer, COUNT(*) AS n
        FROM defendants
        WHERE supervising_officer IS NOT NULL
          AND case_status LIKE 'open%'
        GROUP BY supervising_officer
        ORDER BY n DESC
    """)
    rows = [{"officer": _fmt_officer(r[0]), "email": r[0], "count": r[1]} for r in c.fetchall()]
    max_n = max((r["count"] for r in rows), default=1)
    for r in rows:
        r["pct"] = int(100 * r["count"] / max_n) if max_n else 0
    return rows


def caseload_by_letter() -> list[dict]:
    """Open-case active-roster defendants bucketed by the first letter of
    defendant_name. Names typically arrive as 'LAST, FIRST', so this is
    effectively a last-name letter histogram."""
    c = get_conn().cursor()
    c.execute("""
        SELECT UPPER(SUBSTRING(LTRIM(defendant_name), 1, 1)) AS letter, COUNT(*) AS n
        FROM defendants
        WHERE source IN ('blue_book','both')
          AND case_status LIKE 'open%'
          AND defendant_name IS NOT NULL
          AND LTRIM(defendant_name) <> ''
        GROUP BY UPPER(SUBSTRING(LTRIM(defendant_name), 1, 1))
    """)
    buckets: dict[str, int] = {}
    for letter, n in c.fetchall():
        key = letter if letter and letter.isalpha() else "#"
        buckets[key] = buckets.get(key, 0) + n
    rows = [{"letter": k, "count": v} for k, v in buckets.items()]
    rows.sort(key=lambda r: (r["letter"] == "#", r["letter"]))
    max_n = max((r["count"] for r in rows), default=1)
    for r in rows:
        r["pct"] = int(100 * r["count"] / max_n) if max_n else 0
    return rows


def recent_activity(limit: int = 8) -> list[dict]:
    """Blend the most recent payments, check-ins and GPS events into one feed."""
    c = get_conn().cursor()
    items: list[dict] = []
    c.execute(f"""SELECT TOP {limit} idn, defendant, payment_date, payment_amount, payment_type
                  FROM payments
                  WHERE payment_date IS NOT NULL
                  ORDER BY TRY_CONVERT(datetime2, payment_date) DESC""")
    for idn, d, dt, amt, pt in c.fetchall():
        items.append({
            "idn":  str(idn) if idn else "",
            "kind": "payment",
            "when": _fmt_date(dt),
            "title": f"Payment Processed: ${_d(amt):,.0f}",
            "desc": f"{pt or 'Payment'} received from {d or f'Defendant {idn}'}",
            "sort": str(dt) or "",
        })
    c.execute(f"""SELECT TOP {limit} idn, defendant, check_in_date, type_of_check_in, supervising_officer
                  FROM check_ins
                  WHERE check_in_date IS NOT NULL
                  ORDER BY TRY_CONVERT(datetime2, check_in_date) DESC""")
    for idn, d, dt, t, off in c.fetchall():
        items.append({
            "idn":  str(idn) if idn else "",
            "kind": "checkin",
            "when": _fmt_date(dt),
            "title": f"Check-In Recorded: {idn}",
            "desc": f"{t or 'Check-in'} with {_fmt_officer(off) or 'officer'}",
            "sort": str(dt) or "",
        })
    c.execute(f"""SELECT TOP {limit} idn, defendant, gps_install_date, gps_type, case_status
                  FROM gps_events
                  WHERE gps_install_date IS NOT NULL
                  ORDER BY TRY_CONVERT(datetime2, gps_install_date) DESC""")
    for idn, d, dt, t, s in c.fetchall():
        items.append({
            "idn":  str(idn) if idn else "",
            "kind": "gps",
            "when": _fmt_date(dt),
            "title": f"GPS Installed: {d or idn}",
            "desc": f"{t or 'GPS'} monitor activated — case {s or 'OPEN'}",
            "sort": str(dt) or "",
        })
    items.sort(key=lambda x: x["sort"], reverse=True)
    return items[:limit]


def analytics_bundle() -> dict:
    """Inputs for the six charts on analytics.html."""
    c = get_conn().cursor()
    out: dict = {}

    # Supervision levels
    c.execute("""SELECT pretrial_level, COUNT(*) FROM defendants
                 WHERE pretrial_level IN (1,2,3) AND case_status LIKE 'open%'
                 GROUP BY pretrial_level ORDER BY pretrial_level""")
    out["levels"] = [{"level": r[0], "count": r[1]} for r in c.fetchall()]

    # Officer caseloads
    ocs = officer_caseloads()
    out["officers"] = [{"name": r["officer"], "count": r["count"]} for r in ocs]

    # Check-in types
    c.execute("""SELECT type_of_check_in, COUNT(*) FROM check_ins
                 WHERE type_of_check_in IS NOT NULL
                 GROUP BY type_of_check_in ORDER BY COUNT(*) DESC""")
    out["checkins"] = [{"type": r[0], "count": r[1]} for r in c.fetchall()]

    # GPS types
    c.execute("""SELECT gps_type, COUNT(*) FROM gps_events
                 WHERE gps_type IS NOT NULL
                 GROUP BY gps_type ORDER BY COUNT(*) DESC""")
    out["gps_types"] = [{"type": r[0], "count": r[1]} for r in c.fetchall()]

    # Payments by type (money $)
    c.execute("""SELECT payment_type, COUNT(*), SUM(payment_amount) FROM payments
                 WHERE payment_type IS NOT NULL
                 GROUP BY payment_type ORDER BY SUM(payment_amount) DESC""")
    out["payments"] = [{"type": r[0], "count": r[1], "total": _d(r[2])} for r in c.fetchall()]

    # Compliance trend (derive from check-ins by YYYY-MM for last 12 months)
    c.execute("""
        SELECT FORMAT(TRY_CONVERT(datetime2, check_in_date), 'yyyy-MM') AS ym, COUNT(*) AS n
        FROM check_ins
        WHERE check_in_date IS NOT NULL
          AND TRY_CONVERT(datetime2, check_in_date) >= DATEADD(month, -12, GETDATE())
        GROUP BY FORMAT(TRY_CONVERT(datetime2, check_in_date), 'yyyy-MM')
        ORDER BY ym
    """)
    out["checkin_trend"] = [{"month": r[0], "count": r[1]} for r in c.fetchall() if r[0]]

    return out


# ─── Big one: full defendant bundle for pretrial_app.html ───────────────────

def _coerce_level(v):
    if v is None: return ""
    try: return str(int(v))
    except: return str(v)


def case_management_bundle() -> dict:
    """
    Returns {'defendants':[...], 'stats':{...}} in the exact shape that
    pretrial_app.html's `const RAW` expects.
    """
    c = get_conn().cursor(as_dict=True)

    # 1) defendants - active roster (blue_book or both), sorted by name.
    c.execute("""
        SELECT idn, defendant_name, defendant_last_name, pretrial_level,
               referral_date, closed_date, case_status, charge_type, gps,
               supervising_officer
        FROM dbo.defendants
        WHERE source IN ('blue_book','both')
    """)
    defs_rows = c.fetchall()

    # 2) cases (multi-case per defendant) - join into a CSV string
    c.execute("""SELECT idn, case_number FROM dbo.cases""")
    cases_by_idn: dict[int, list[str]] = {}
    for r in c.fetchall():
        cases_by_idn.setdefault(r["idn"], []).append(r["case_number"])

    # 3) check-ins
    c.execute("""SELECT idn, check_in_date, type_of_check_in, supervising_officer,
                        case_status, pretrial_level
                 FROM dbo.check_ins""")
    ci_by_idn: dict[int, list[dict]] = {}
    for r in c.fetchall():
        ci_by_idn.setdefault(r["idn"], []).append({
            "date":   _fmt_date(r["check_in_date"]),
            "type":   r["type_of_check_in"] or "",
            "officer": _fmt_officer(r["supervising_officer"]) or "",
            "status": r["case_status"] or "",
            "level":  _coerce_level(r["pretrial_level"]),
        })

    # 4) payments
    c.execute("""SELECT idn, payment_date, payment_amount, officer, payment_type
                 FROM dbo.payments""")
    pay_by_idn: dict[int, list[dict]] = {}
    for r in c.fetchall():
        pay_by_idn.setdefault(r["idn"], []).append({
            "date":   _fmt_date(r["payment_date"]),
            "amount": _d(r["payment_amount"]),
            "officer": _fmt_officer(r["officer"]) or (r["officer"] or ""),
            "type":   r["payment_type"] or "",
        })

    # 5) GPS events
    c.execute("""SELECT idn, gps_type, case_status, paid, victim, victim_idn,
                        victim_time_48, victim_accept_deny_gps, gps_install_date,
                        da_emailed, case_number, referral_date
                 FROM dbo.gps_events""")
    gps_by_idn: dict[int, list[dict]] = {}
    for r in c.fetchall():
        gps_by_idn.setdefault(r["idn"], []).append({
            "gpsType":      r["gps_type"] or "",
            "status":       r["case_status"] or "",
            "paid":         bool(r["paid"]) if r["paid"] is not None else None,
            "victim":       r["victim"] or "",
            "victimIDN":    r["victim_idn"] or "",
            "victimTime":   _fmt_date(r["victim_time_48"]),
            "victimAccept": bool(r["victim_accept_deny_gps"]) if r["victim_accept_deny_gps"] is not None else None,
            "installDate":  _fmt_date(r["gps_install_date"]),
            "daEmailed":    bool(r["da_emailed"]) if r["da_emailed"] is not None else None,
            "caseNum":      r["case_number"] or "",
            "referral":     _fmt_date(r["referral_date"]),
        })

    # Stitch into the mockup's expected shape
    defendants = []
    total_paid_sum = 0.0
    ci_total = 0
    gps_active = 0
    for r in defs_rows:
        idn = r["idn"]
        name = r["defendant_name"] or r["defendant_last_name"] or f"Defendant {idn}"
        display = name.title() if name else ""
        case_nums = cases_by_idn.get(idn, [])
        case_num_str = ", ".join(f"@{c}" for c in case_nums)
        checkins = ci_by_idn.get(idn, [])
        payments = pay_by_idn.get(idn, [])
        gps_recs = gps_by_idn.get(idn, [])
        if r["gps"]:
            gps_active += 1
        ci_total += len(checkins)
        total_paid_sum += sum(p["amount"] for p in payments)

        defendants.append({
            "idn":         str(idn),
            "name":        name,
            "displayName": display,
            "caseNum":     case_num_str,
            "level":       _coerce_level(r["pretrial_level"]),
            "referral":    _fmt_date(r["referral_date"]) or "",
            "status":      (r["case_status"] or "Open").title(),
            "chargeType":  r["charge_type"] or "",
            "gps":         bool(r["gps"]) if r["gps"] is not None else False,
            "officer":     _fmt_officer(r["supervising_officer"]) or "",
            "victim":      (gps_recs[0]["victim"] if gps_recs else ""),
            "closedDate":  _fmt_date(r["closed_date"]) or "",
            "gpsType":     (gps_recs[0]["gpsType"] if gps_recs else ""),
            "modified":    "",
            "checkIns":    checkins,
            "payments":    payments,
            "gpsRecords":  gps_recs,
        })

    defendants.sort(key=lambda d: (d["name"] or "").upper())

    # Overall stats
    open_n = sum(1 for d in defendants if d["status"].lower().startswith("open"))
    closed_n = sum(1 for d in defendants if d["status"].lower().startswith("closed"))
    stats = {
        "total":         len(defendants),
        "open":          open_n,
        "closed":        closed_n,
        "gpsActive":     gps_active,
        "totalPaid":     round(total_paid_sum, 2),
        "checkInsTotal": ci_total,
    }
    return {"defendants": defendants, "stats": stats}


def _now_iso() -> str:
    return datetime.utcnow().strftime("%Y-%m-%d %H:%M:%S")


def defendant_exists(idn: int) -> bool:
    c = get_conn().cursor()
    c.execute("SELECT 1 FROM dbo.defendants WHERE idn=%s", (int(idn),))
    return c.fetchone() is not None


def insert_referral(d: dict) -> dict:
    """Create a new defendant + (optionally) a row in cases.
    Required: idn (int), defendant_name (str).
    Returns {'ok': True, 'idn': ...} or {'ok': False, 'error': ...}."""
    try:
        idn = int(str(d.get("idn") or "").strip())
    except ValueError:
        return {"ok": False, "error": "IDN must be a number"}
    name = (d.get("defendant_name") or "").strip()
    if not name:
        return {"ok": False, "error": "Defendant name is required"}
    if defendant_exists(idn):
        return {"ok": False, "error": f"IDN {idn} already exists. Use the existing record instead of creating a duplicate."}

    conn = get_conn()
    c = conn.cursor()

    last_name = name.split(",", 1)[0].strip().upper() if "," in name else name.upper()
    pretrial_level = d.get("pretrial_level") or None
    bond_amount = d.get("bond_amount") or None
    referral_date = (d.get("referral_date") or "").strip() or _now_iso()
    closed_date = None

    c.execute("""
        INSERT INTO dbo.defendants
            (idn, defendant_name, defendant_last_name, birthdate,
             pretrial_level, charge_type, supervision_type, order_from,
             dma, gps, referral_date, closed_date, case_status,
             supervising_officer, ptr_successfully_completed,
             bond_amount, total_paid, source)
        VALUES (%s, %s, %s, %s,
                %s, %s, %s, %s,
                %s, %s, %s, %s, %s,
                %s, %s,
                %s, %s, %s)
    """, (
        idn, name, last_name, (d.get("birthdate") or None),
        (int(pretrial_level) if pretrial_level else None),
        (d.get("charge_type") or None),
        (d.get("supervision_type") or None),
        (d.get("order_from") or None),
        bool(d.get("dma")),
        bool(d.get("gps")),
        referral_date, closed_date, "Open",
        (d.get("supervising_officer") or None),
        None,
        (float(bond_amount) if bond_amount else None),
        0.0,
        "blue_book",
    ))

    case_number = (d.get("case_number") or "").strip()
    if case_number:
        for cn in [s.strip().lstrip("@") for s in case_number.split(",") if s.strip()]:
            c.execute("""INSERT INTO dbo.cases (idn, case_number, source)
                         VALUES (%s, %s, %s)""", (idn, cn, "blue_book"))

    conn.commit()
    _cache.clear()
    return {"ok": True, "idn": idn}


def insert_check_in(d: dict) -> dict:
    try:
        idn = int(str(d.get("idn") or "").strip())
    except ValueError:
        return {"ok": False, "error": "IDN must be a number"}
    if not defendant_exists(idn):
        return {"ok": False, "error": f"IDN {idn} not found. Create a referral first."}

    conn = get_conn()
    c = conn.cursor()
    c.execute("""
        INSERT INTO dbo.check_ins
            (idn, case_number, defendant, check_in_date, type_of_check_in,
             supervising_officer, case_status, referral_date, pretrial_level)
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
    """, (
        idn,
        (d.get("case_number") or None),
        (d.get("defendant") or None),
        (d.get("check_in_date") or _now_iso()),
        (d.get("type_of_check_in") or None),
        (d.get("supervising_officer") or None),
        "Open",
        None,
        None,
    ))
    conn.commit()
    _cache.clear()
    return {"ok": True}


def insert_payment(d: dict) -> dict:
    try:
        idn = int(str(d.get("idn") or "").strip())
    except ValueError:
        return {"ok": False, "error": "IDN must be a number"}
    if not defendant_exists(idn):
        return {"ok": False, "error": f"IDN {idn} not found. Create a referral first."}
    try:
        amount = float(d.get("payment_amount") or 0)
    except ValueError:
        return {"ok": False, "error": "Payment amount must be a number"}
    if amount <= 0:
        return {"ok": False, "error": "Payment amount must be greater than 0"}

    conn = get_conn()
    c = conn.cursor()
    c.execute("""
        INSERT INTO dbo.payments
            (idn, case_number, defendant, payment_date, payment_amount,
             officer, payment_type, case_status)
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s)
    """, (
        idn,
        (d.get("case_number") or None),
        (d.get("defendant") or None),
        (d.get("payment_date") or _now_iso()),
        amount,
        (d.get("officer") or None),
        (d.get("payment_type") or None),
        "Open",
    ))
    conn.commit()
    _cache.clear()
    return {"ok": True}


def get_defendant_details(idn: int) -> dict | None:
    """Bundle for the slide-in drawer: defendant + recent check-ins + recent
    payments + GPS info. Single endpoint, single trip."""
    base = get_defendant_full(idn)
    if not base:
        return None
    c = get_conn().cursor(as_dict=True)
    c.execute("""SELECT TOP 25 check_in_date, type_of_check_in, supervising_officer
                 FROM dbo.check_ins WHERE idn=%s
                 ORDER BY TRY_CONVERT(datetime2, check_in_date) DESC""", (int(idn),))
    check_ins = [{
        "date":    _fmt_date(r["check_in_date"]),
        "type":    r["type_of_check_in"] or "",
        "officer": _fmt_officer(r["supervising_officer"]) or (r["supervising_officer"] or ""),
    } for r in c.fetchall()]

    c.execute("""SELECT TOP 25 payment_date, payment_amount, officer, payment_type
                 FROM dbo.payments WHERE idn=%s
                 ORDER BY TRY_CONVERT(datetime2, payment_date) DESC""", (int(idn),))
    payments = [{
        "date":    _fmt_date(r["payment_date"]),
        "amount":  _d(r["payment_amount"]),
        "officer": _fmt_officer(r["officer"]) or (r["officer"] or ""),
        "type":    r["payment_type"] or "",
    } for r in c.fetchall()]

    c.execute("""SELECT gps_type, case_status, victim, victim_accept_deny_gps,
                        gps_install_date, da_emailed, court_order
                 FROM dbo.gps_events WHERE idn=%s""", (int(idn),))
    gps_rows = c.fetchall()
    gps = None
    if gps_rows:
        r = gps_rows[0]
        gps = {
            "type":    r["gps_type"] or "",
            "status":  r["case_status"] or "",
            "victim":  r["victim"] or "",
            "accept":  ("Yes" if r["victim_accept_deny_gps"] == 1
                       else "No" if r["victim_accept_deny_gps"] == 0 else ""),
            "install": _fmt_date(r["gps_install_date"]),
            "daEmailed": ("Yes" if r["da_emailed"] == 1
                         else "No" if r["da_emailed"] == 0 else ""),
            "order":   r["court_order"] or "",
        }

    total_paid = sum(p["amount"] for p in payments)
    return {
        **base,
        "check_ins":    check_ins,
        "payments":     payments,
        "gps_details":  gps,
        "totals": {
            "paid":          round(total_paid, 2),
            "check_in_count": len(check_ins),
            "payment_count":  len(payments),
        },
    }


def get_defendant_full(idn: int) -> dict | None:
    """Return all editable fields for one defendant (and their case numbers)."""
    c = get_conn().cursor(as_dict=True)
    c.execute("""
        SELECT idn, defendant_name, defendant_last_name, birthdate,
               pretrial_level, charge_type, supervision_type, order_from,
               dma, gps, referral_date, closed_date, case_status,
               supervising_officer, ptr_successfully_completed,
               bond_amount, total_paid, source
        FROM dbo.defendants WHERE idn = %s
    """, (int(idn),))
    row = c.fetchone()
    if not row:
        return None
    c.execute("SELECT case_number FROM dbo.cases WHERE idn = %s ORDER BY case_id", (int(idn),))
    cases = [r["case_number"] for r in c.fetchall()]
    return {
        "idn":             row["idn"],
        "defendant_name":  row["defendant_name"] or "",
        "birthdate":       row["birthdate"] or "",
        "pretrial_level":  _coerce_level(row["pretrial_level"]),
        "charge_type":     row["charge_type"] or "",
        "supervision_type": row["supervision_type"] or "",
        "order_from":      row["order_from"] or "",
        "dma":             bool(row["dma"]) if row["dma"] is not None else False,
        "gps":             bool(row["gps"]) if row["gps"] is not None else False,
        "referral_date":   row["referral_date"] or "",
        "closed_date":     row["closed_date"] or "",
        "case_status":     row["case_status"] or "",
        "supervising_officer": row["supervising_officer"] or "",
        "ptr_successfully_completed": bool(row["ptr_successfully_completed"]) if row["ptr_successfully_completed"] is not None else False,
        "bond_amount":     _d(row["bond_amount"]),
        "total_paid":      _d(row["total_paid"]),
        "case_numbers":    ", ".join(f"@{c}" for c in cases),
    }


# Whitelist of columns that the edit form is allowed to update.
_EDITABLE_DEFENDANT_COLS = {
    "defendant_name":  "NVARCHAR",
    "birthdate":       "NVARCHAR",
    "pretrial_level":  "INT",
    "charge_type":     "NVARCHAR",
    "supervision_type": "NVARCHAR",
    "order_from":      "NVARCHAR",
    "dma":             "BIT",
    "gps":             "BIT",
    "referral_date":   "NVARCHAR",
    "closed_date":     "NVARCHAR",
    "case_status":     "NVARCHAR",
    "supervising_officer": "NVARCHAR",
    "ptr_successfully_completed": "BIT",
    "bond_amount":     "DECIMAL",
}


def update_defendant(idn: int, body: dict) -> dict:
    """Apply a partial update to a defendant row. Only whitelisted columns
    are honored. Returns {ok, changed_cols} or {ok: False, error}."""
    try:
        idn = int(str(idn).strip())
    except ValueError:
        return {"ok": False, "error": "IDN must be a number"}
    if not defendant_exists(idn):
        return {"ok": False, "error": f"IDN {idn} not found"}

    sets, vals, changed = [], [], []
    for col, kind in _EDITABLE_DEFENDANT_COLS.items():
        if col not in body:
            continue
        v = body[col]
        if v == "":
            v = None
        elif kind == "INT":
            try: v = int(v) if v is not None else None
            except (ValueError, TypeError): return {"ok": False, "error": f"{col} must be a number"}
        elif kind == "DECIMAL":
            try: v = float(v) if v is not None else None
            except (ValueError, TypeError): return {"ok": False, "error": f"{col} must be a number"}
        elif kind == "BIT":
            v = bool(v)
        sets.append(f"{col} = %s")
        vals.append(v)
        changed.append(col)

    if not sets:
        return {"ok": False, "error": "No editable fields supplied"}

    conn = get_conn()
    c = conn.cursor()
    sql = f"UPDATE dbo.defendants SET {', '.join(sets)} WHERE idn = %s"
    vals.append(idn)
    c.execute(sql, tuple(vals))

    # Optional: if case_numbers provided, sync cases table.
    if "case_numbers" in body:
        new_cases = [s.strip().lstrip("@") for s in (body["case_numbers"] or "").split(",") if s.strip()]
        c.execute("SELECT case_number FROM dbo.cases WHERE idn = %s", (idn,))
        existing = {r[0] for r in c.fetchall()}
        wanted = set(new_cases)
        for cn in wanted - existing:
            c.execute("INSERT INTO dbo.cases (idn, case_number, source) VALUES (%s, %s, %s)",
                      (idn, cn, "edit"))
        for cn in existing - wanted:
            c.execute("DELETE FROM dbo.cases WHERE idn = %s AND case_number = %s", (idn, cn))
        changed.append("case_numbers")

    conn.commit()
    _cache.clear()
    return {"ok": True, "changed_cols": changed}


def defendant_lookup(q: str, limit: int = 10) -> list[dict]:
    """Live-search defendants by name fragment OR IDN prefix.
    Returns the active roster (blue_book/both) flagged as 'active' if case is open.
    Used by data-entry pages to surface duplicates as the user types."""
    q = (q or "").strip()
    if len(q) < 2:
        return []
    c = get_conn().cursor()
    # If purely digits, prefer IDN prefix; otherwise name LIKE.
    if q.isdigit():
        c.execute(f"""
            SELECT TOP {int(limit)} d.idn, d.defendant_name, d.case_status,
                   d.pretrial_level, d.supervising_officer, d.referral_date,
                   (SELECT TOP 1 CONCAT('@', case_number) FROM dbo.cases x WHERE x.idn = d.idn) AS case_no
            FROM dbo.defendants d
            WHERE d.source IN ('blue_book','both')
              AND CAST(d.idn AS NVARCHAR(20)) LIKE %s
            ORDER BY d.case_status DESC, d.defendant_name
        """, (q + "%",))
    else:
        c.execute(f"""
            SELECT TOP {int(limit)} d.idn, d.defendant_name, d.case_status,
                   d.pretrial_level, d.supervising_officer, d.referral_date,
                   (SELECT TOP 1 CONCAT('@', case_number) FROM dbo.cases x WHERE x.idn = d.idn) AS case_no
            FROM dbo.defendants d
            WHERE d.source IN ('blue_book','both')
              AND d.defendant_name LIKE %s
            ORDER BY d.case_status DESC, d.defendant_name
        """, ("%" + q.upper() + "%",))
    rows = []
    for idn, name, status, lvl, off, ref, case_no in c.fetchall():
        is_open = bool(status and str(status).lower().startswith("open"))
        rows.append({
            "idn":      str(idn),
            "name":     (name or "").strip(),
            "status":   (status or "").title(),
            "active":   is_open,
            "level":    _coerce_level(lvl),
            "officer":  _fmt_officer(off) or "",
            "referral": _fmt_date(ref) or "",
            "caseNum":  case_no or "",
        })
    return rows


def defendants_for_dropdown() -> list[dict]:
    c = get_conn().cursor()
    # Join on the first case per defendant for the dropdown's caseNum preview
    c.execute("""
        SELECT d.idn, d.defendant_name, d.pretrial_level, d.supervising_officer,
               (SELECT TOP 1 CONCAT('@', case_number) FROM dbo.cases c WHERE c.idn = d.idn) AS case_num
        FROM dbo.defendants d
        WHERE d.source IN ('blue_book','both') AND d.defendant_name IS NOT NULL
        ORDER BY d.defendant_name
    """)
    rows = []
    for idn, name, lvl, off, case_num in c.fetchall():
        officer = _fmt_officer(off)
        # Match mockup's officer convention (last name only, or "(unassigned)")
        off_disp = officer.split()[-1] if officer else "(unassigned)"
        rows.append({
            "idn": str(idn),
            "name": name,
            "caseNum": case_num or "",
            "level": f"Level {_coerce_level(lvl)}" if _coerce_level(lvl) in ("1","2","3") else (f"Level {_coerce_level(lvl)}" if _coerce_level(lvl) else ""),
            "officer": off_disp,
        })
    return rows


# ─── Client Profile lookup bundle (for client_profile.html) ─────────────────

def client_profiles_bundle() -> dict:
    """Returns {'clients': [...]} in the exact shape the client_profile.html
    mockup expects (replaces its CSV-upload path)."""
    c = get_conn().cursor(as_dict=True)

    # All defendants in the active roster
    c.execute("""
        SELECT d.idn, d.defendant_name, d.defendant_last_name, d.birthdate,
               d.pretrial_level, d.charge_type, d.supervision_type, d.order_from,
               d.dma, d.gps, d.referral_date, d.closed_date, d.case_status,
               d.supervising_officer, d.ptr_successfully_completed,
               d.bond_amount, d.total_paid,
               (SELECT TOP 1 CONCAT('@', case_number) FROM dbo.cases c WHERE c.idn=d.idn) AS case_no,
               rbb.day_adjustment AS day_adj,
               rbb.victim          AS bb_victim
        FROM dbo.defendants d
        LEFT JOIN dbo.raw_blue_book rbb ON rbb.idn = d.idn
        WHERE d.source IN ('blue_book','both')
    """)
    defs = c.fetchall()

    # check-ins
    c.execute("""SELECT idn, check_in_date, type_of_check_in, supervising_officer
                 FROM dbo.check_ins""")
    ci_by_idn: dict[int, list[dict]] = {}
    for r in c.fetchall():
        ci_by_idn.setdefault(r["idn"], []).append({
            "date":    _fmt_date(r["check_in_date"]) or "",
            "type":    r["type_of_check_in"] or "",
            "officer": _fmt_officer(r["supervising_officer"]) or (r["supervising_officer"] or ""),
        })

    # payments (with ISO date for JS Date() parsing - keep raw iso in 'iso' so coverage math works)
    c.execute("""SELECT idn, payment_date, payment_amount, officer, payment_type
                 FROM dbo.payments""")
    pm_by_idn: dict[int, list[dict]] = {}
    for r in c.fetchall():
        pm_by_idn.setdefault(r["idn"], []).append({
            "date":    _fmt_date(r["payment_date"]) or "",
            "amount":  _d(r["payment_amount"]),
            "type":    r["payment_type"] or "",
            "officer": _fmt_officer(r["officer"]) or (r["officer"] or ""),
        })

    # GPS (one per idn)
    c.execute("""SELECT idn, gps_type, case_status, victim, victim_accept_deny_gps,
                        gps_install_date, da_emailed, court_order
                 FROM dbo.gps_events""")
    gp_by_idn: dict[int, dict] = {}
    for r in c.fetchall():
        gp_by_idn[r["idn"]] = {
            "gpsType":  r["gps_type"] or "",
            "status":   r["case_status"] or "",
            "victim":   r["victim"] or "",
            "accept":   ("Yes" if r["victim_accept_deny_gps"] == 1
                         else "No" if r["victim_accept_deny_gps"] == 0 else ""),
            "install":  _fmt_date(r["gps_install_date"]) or "",
            "install_iso": r["gps_install_date"] or "",  # for JS Date() parsing
            "daEmailed": ("Yes" if r["da_emailed"] == 1
                          else "No" if r["da_emailed"] == 0 else ""),
            "order":     r["court_order"] or "",
        }

    clients = []
    for d in defs:
        idn = d["idn"]
        gp = gp_by_idn.get(idn)
        gps_active = bool(d["gps"]) or gp is not None
        clients.append({
            "idn":       str(idn),
            "name":      d["defendant_name"] or d["defendant_last_name"] or "",
            "status":    (d["case_status"] or "").title() if d["case_status"] else "",
            "level":     _coerce_level(d["pretrial_level"]),
            "officer":   _fmt_officer(d["supervising_officer"]) or "",
            "suptype":   d["supervision_type"] or "",
            "charge":    d["charge_type"] or "",
            "order":     d["order_from"] or "",
            "refdate":   _fmt_date(d["referral_date"]) or "",
            "closed":    _fmt_date(d["closed_date"]) or "",
            "bond":      _d(d["bond_amount"]),
            "gpsActive": gps_active,
            "gpsType":   (gp["gpsType"] if gp else ""),
            "dma":       ("Yes" if d["dma"] == 1 else "No" if d["dma"] == 0 else ""),
            "bday":      _fmt_date(d["birthdate"]) or "",
            "caseNo":    d["case_no"] or "",
            "ptrDone":   ("Yes" if d["ptr_successfully_completed"] == 1
                          else "No" if d["ptr_successfully_completed"] == 0 else ""),
            "dayAdj":    int(d["day_adj"] or 0),
            "victim":    (gp["victim"] if gp and gp["victim"] else (d["bb_victim"] or "")),
            "gpInstall": (gp["install_iso"] if gp else ""),
            "gpVictim":  (gp["accept"] if gp else ""),
            "gpOrder":   (gp["order"] if gp else ""),
            "gpDA":      (gp["daEmailed"] if gp else ""),
            "gpStatus":  (gp["status"] if gp else ""),
            "checkIns":  ci_by_idn.get(idn, []),
            "payments":  pm_by_idn.get(idn, []),
            # Column hint objects - the renderer uses these to pull fields from rows.
            "_ci": {"dateCol":"date","typeCol":"type","offCol":"officer"},
            "_pm": {"amtCol":"amount","dateCol":"date","typeCol":"type","offCol":"officer"},
        })

    clients.sort(key=lambda c: (c["name"] or "").upper())
    return {"clients": clients}
