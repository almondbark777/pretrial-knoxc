"""
queries_ext.py — Extended queries for the new feature set added in
migration 001 (notes, tags, court dates, audit log, screens, violations,
saved searches, pinned defendants, user preferences, reminders, documents).

Imported by app.py alongside queries.py. Lives in a separate file purely
for organization — the original queries.py is already 900+ lines.
"""
from __future__ import annotations
import json
from datetime import datetime, timedelta

from queries import (
    get_conn, _cache, _fmt_date, _fmt_officer, _d, defendant_exists,
)


# ───────────────────────────────────────────────────────────── DATE HELPERS ───

def _to_iso(s):
    if not s:
        return None
    if isinstance(s, datetime):
        return s.isoformat(sep=" ", timespec="seconds")
    return str(s)


# ──────────────────────────────────────────── PTR CLIENT LOOKUP DATASETS ───
# Feeds the bundled "PTR Client Lookup" single-page app directly from SQL so
# officers no longer upload SharePoint CSVs. The app discovers columns by name
# (colFind), so we return the raw_* tables with their keys renamed back to the
# exact SharePoint headers the app expects, and every value coerced to a string
# (mimicking PapaParse's all-strings output). buildClients() in the app then
# consumes these unchanged.
#
# NOTE: raw_gps_48_hours in the current DB does NOT carry the "Switched To",
# "Switched GPS Date" or "Notes" columns, so switch-aware billing, GPS-relief
# freezing, and the fee-waiver banner degrade gracefully (colFind -> null). Add
# those columns to the table + ETL to restore them.

# snake_case DB column  ->  SharePoint header the app's colFind expects
_BB_MAP = {
    "idn": "IDN",
    "defendant": "Defendant",
    "case_status": "Case Status",
    "pretrial_level": "Pretrial Level ",   # trailing space matches real export
    "supervising_officer": "Supervising Officer",
    "supervision_type": "Supervision Type",
    "charge_type": "Charge Type",
    "order_from": "Order From",
    "referral_date": "Referral Date",
    "closed_date": "Closed Date",
    "bond_amount": "Bond Amount",
    "gps": "GPS",
    "gps_type": "GPS Type",
    "dma": "DMA",
    "birthdate": "Birthdate",
    "warrant_case_num": "Warrant/Case #",
    "ptr_successfully_completed": "PTR Successfully Completed?",
    "victim": "Victim",
    "day_adjustment": "Day Adjustment",
}
_CI_MAP = {
    "idn": "IDN",
    "case_number": "Case Number",
    "defendant": "Defendant",
    "date": "Check in Date",
    "type_of_check_in": "Type of check in",
    "supervising_officer": "Supervising Officer",
    "case_status": "Case Status",
    "referral_date": "Referral Date",
    "pretrial_level": "Pretrial Level ",
}
_PM_MAP = {
    "idn": "IDN",
    "case_number": "Case Number",
    "defendant": "Defendant",
    "payment_date": "Payment Date",
    "payment_amount": "Payment Amount",
    "officer_that_collected_payment": "Officer That Collected Payment",
    "payment_type": "Payment Type",
    "case_status": "Case Status",
}
_GP_MAP = {
    "idn": "IDN",
    "case_number": "Case Number",
    "defendant": "Defendant",
    "referral_date": "Referral Date",
    "gps_type": "GPS Type",
    "case_status": "Case Status",
    "paid": "Paid",
    "victim": "Victim",
    "victim_accept_deny_gps": "Victim Accept/Deny GPS",
    "gps_install_date": "GPS Install Date",
    "order": "Order",
    "da_emailed": "DA Emailed",
    "closed_date": "Closed Date",
    "switched_to": "Switched To",
    "switched_gps_date": "Switched GPS Date",
    "notes": "Notes",
}


def _ls_str(v) -> str:
    """Coerce any SQL value to the string form PapaParse would have produced."""
    if v is None:
        return ""
    if isinstance(v, bool):
        return "1" if v else "0"
    return str(v)


def _ls_rows(table: str, colmap: dict) -> list[dict]:
    c = get_conn().cursor(as_dict=True)
    c.execute(f"SELECT * FROM dbo.{table}")
    out = []
    for r in c.fetchall():
        # Emit EVERY mapped header, even when the underlying SQL column is
        # absent (-> ""), so the app's colFind() discovers a stable, complete
        # set of columns regardless of which optional columns this DB happens
        # to carry. A real CSV always has all its headers; this mirrors that.
        # Without this, a missing raw_gps_48_hours."switched_to"/"switched_gps_date"/
        # "notes" column made colFind() return null and silently disabled
        # switch-aware billing, GPS-relief freezing, and the fee-waiver banner
        # (PHASE_2 finding R1). NOTE: this only makes discovery deterministic —
        # those features still require the source data to actually be present.
        row = {header: _ls_str(r.get(snake)) for snake, header in colmap.items()}
        out.append(row)
    return out


def lookup_datasets() -> dict:
    """The four datasets the PTR Client Lookup app needs, keyed bb/ci/pm/gp."""
    return {
        "bb": _ls_rows("raw_blue_book", _BB_MAP),
        "ci": _ls_rows("raw_check_ins", _CI_MAP),
        "pm": _ls_rows("raw_payments", _PM_MAP),
        "gp": _ls_rows("raw_gps_48_hours", _GP_MAP),
    }


# ─────────────────────────────────────────────────────────────────── NOTES ───

def list_notes(idn: int) -> list[dict]:
    c = get_conn().cursor()
    c.execute("""
        SELECT note_id, author, body, created_at
        FROM dbo.defendant_notes
        WHERE idn = %s
        ORDER BY created_at DESC
    """, (int(idn),))
    return [{
        "id":     r[0],
        "author": _fmt_officer(r[1]) or (r[1] or ""),
        "body":   r[2] or "",
        "when":   _to_iso(r[3]),
    } for r in c.fetchall()]


def add_note(idn: int, author: str, body: str) -> dict:
    if not body or not body.strip():
        return {"ok": False, "error": "Note body required"}
    if not defendant_exists(int(idn)):
        return {"ok": False, "error": "Defendant not found"}
    conn = get_conn(); c = conn.cursor()
    c.execute("INSERT INTO dbo.defendant_notes (idn, author, body) VALUES (%s, %s, %s)",
              (int(idn), author or None, body.strip()))
    conn.commit()
    _cache.clear()
    return {"ok": True}


def delete_note(note_id: int, user: str) -> dict:
    conn = get_conn(); c = conn.cursor()
    c.execute("DELETE FROM dbo.defendant_notes WHERE note_id = %s", (int(note_id),))
    conn.commit()
    return {"ok": True}


# ──────────────────────────────────────────────────────────────────── TAGS ───

def list_tags(idn: int) -> list[dict]:
    c = get_conn().cursor()
    c.execute("""
        SELECT tag_id, label, author, created_at
        FROM dbo.defendant_tags WHERE idn = %s
        ORDER BY created_at DESC
    """, (int(idn),))
    return [{"id": r[0], "label": r[1], "author": _fmt_officer(r[2]) or "", "when": _to_iso(r[3])}
            for r in c.fetchall()]


def add_tag(idn: int, label: str, author: str) -> dict:
    label = (label or "").strip()
    if not label:
        return {"ok": False, "error": "Tag label required"}
    if not defendant_exists(int(idn)):
        return {"ok": False, "error": "Defendant not found"}
    conn = get_conn(); c = conn.cursor()
    # de-dupe: if the same label already exists for this defendant, no-op
    c.execute("SELECT tag_id FROM dbo.defendant_tags WHERE idn=%s AND LOWER(label)=LOWER(%s)",
              (int(idn), label))
    if c.fetchone():
        return {"ok": True, "duplicate": True}
    c.execute("INSERT INTO dbo.defendant_tags (idn, label, author) VALUES (%s, %s, %s)",
              (int(idn), label, author or None))
    conn.commit()
    return {"ok": True}


def delete_tag(tag_id: int) -> dict:
    conn = get_conn(); c = conn.cursor()
    c.execute("DELETE FROM dbo.defendant_tags WHERE tag_id = %s", (int(tag_id),))
    conn.commit()
    return {"ok": True}


def all_tag_labels() -> list[dict]:
    """Distinct tag labels with usage count — used for filter UIs."""
    c = get_conn().cursor()
    c.execute("""SELECT label, COUNT(*) FROM dbo.defendant_tags
                 GROUP BY label ORDER BY COUNT(*) DESC""")
    return [{"label": r[0], "count": r[1]} for r in c.fetchall()]


# ──────────────────────────────────────────────────────────── COURT DATES ───

def list_court_dates(idn: int) -> list[dict]:
    c = get_conn().cursor()
    c.execute("""
        SELECT court_date_id, court_date, court, notes, author, created_at
        FROM dbo.court_dates WHERE idn = %s
        ORDER BY court_date
    """, (int(idn),))
    return [{
        "id":     r[0],
        "date":   _to_iso(r[1]),
        "court":  r[2] or "",
        "notes":  r[3] or "",
        "author": _fmt_officer(r[4]) or "",
        "when":   _to_iso(r[5]),
    } for r in c.fetchall()]


def add_court_date(idn: int, date_str: str, court: str, notes: str, author: str) -> dict:
    if not date_str:
        return {"ok": False, "error": "Date required"}
    if not defendant_exists(int(idn)):
        return {"ok": False, "error": "Defendant not found"}
    conn = get_conn(); c = conn.cursor()
    c.execute("""INSERT INTO dbo.court_dates (idn, court_date, court, notes, author)
                 VALUES (%s, %s, %s, %s, %s)""",
              (int(idn), date_str, court or None, notes or None, author or None))
    conn.commit()
    return {"ok": True}


def delete_court_date(court_date_id: int) -> dict:
    conn = get_conn(); c = conn.cursor()
    c.execute("DELETE FROM dbo.court_dates WHERE court_date_id = %s", (int(court_date_id),))
    conn.commit()
    return {"ok": True}


def upcoming_court_dates(days: int = 30) -> list[dict]:
    """Court dates in the next N days. For the calendar page + dashboard."""
    c = get_conn().cursor()
    c.execute("""
        SELECT cd.court_date_id, cd.idn, d.defendant_name, cd.court_date,
               cd.court, cd.notes, d.supervising_officer
        FROM dbo.court_dates cd
        LEFT JOIN dbo.defendants d ON d.idn = cd.idn
        WHERE cd.court_date >= CAST(GETDATE() AS DATETIME2)
          AND cd.court_date <= DATEADD(day, %s, GETDATE())
        ORDER BY cd.court_date
    """, (int(days),))
    return [{
        "id":      r[0],
        "idn":     str(r[1]),
        "name":    r[2] or f"IDN {r[1]}",
        "date":    _to_iso(r[3]),
        "court":   r[4] or "",
        "notes":   r[5] or "",
        "officer": _fmt_officer(r[6]) or "",
    } for r in c.fetchall()]


# ───────────────────────────────────────────────────────────── AUDIT LOG ───

def write_audit(user: str, action: str, table_name: str, row_id, col_name=None,
                old_value=None, new_value=None) -> None:
    """Best-effort audit write — never raises (we don't want audit failures
    to break the user's edit)."""
    try:
        conn = get_conn(); c = conn.cursor()
        c.execute("""
            INSERT INTO dbo.audit_log (user_id, action, table_name, row_id, col_name, old_value, new_value)
            VALUES (%s, %s, %s, %s, %s, %s, %s)
        """, (
            user, action, table_name, str(row_id) if row_id is not None else None,
            col_name,
            None if old_value is None else str(old_value),
            None if new_value is None else str(new_value),
        ))
        conn.commit()
    except Exception:
        pass


def audit_for_defendant(idn: int, limit: int = 25) -> list[dict]:
    c = get_conn().cursor()
    c.execute(f"""
        SELECT TOP {int(limit)} ts, user_id, action, col_name, old_value, new_value
        FROM dbo.audit_log
        WHERE table_name='defendants' AND row_id=%s
        ORDER BY ts DESC
    """, (str(int(idn)),))
    return [{
        "ts":    _to_iso(r[0]),
        "user":  _fmt_officer(r[1]) or (r[1] or ""),
        "action": r[2],
        "col":   r[3] or "",
        "old":   r[4] or "",
        "new":   r[5] or "",
    } for r in c.fetchall()]


# ──────────────────────────────────────────────────────────── VIOLATIONS ───

def list_violations(idn: int = None, limit: int = 50) -> list[dict]:
    c = get_conn().cursor()
    if idn:
        c.execute(f"""SELECT TOP {int(limit)} v.violation_id, v.idn, v.violation_date,
                            v.category, v.severity, v.description, v.action_taken,
                            v.officer, v.court_notified, v.da_notified, v.created_at
                     FROM dbo.violations v WHERE idn=%s
                     ORDER BY violation_date DESC""", (int(idn),))
    else:
        c.execute(f"""SELECT TOP {int(limit)} v.violation_id, v.idn, v.violation_date,
                            v.category, v.severity, v.description, v.action_taken,
                            v.officer, v.court_notified, v.da_notified, v.created_at
                     FROM dbo.violations v
                     ORDER BY violation_date DESC""")
    out = []
    for r in c.fetchall():
        out.append({
            "id":            r[0],
            "idn":           str(r[1]),
            "date":          _to_iso(r[2]),
            "category":      r[3] or "",
            "severity":      r[4] or "",
            "description":   r[5] or "",
            "action_taken":  r[6] or "",
            "officer":       _fmt_officer(r[7]) or (r[7] or ""),
            "court_notified": bool(r[8]),
            "da_notified":    bool(r[9]),
            "created":       _to_iso(r[10]),
        })
    return out


def add_violation(d: dict) -> dict:
    try:
        idn = int(d.get("idn"))
    except (TypeError, ValueError):
        return {"ok": False, "error": "IDN required"}
    if not defendant_exists(idn):
        return {"ok": False, "error": "Defendant not found"}
    conn = get_conn(); c = conn.cursor()
    c.execute("""INSERT INTO dbo.violations
                 (idn, violation_date, category, severity, description, action_taken,
                  officer, court_notified, da_notified)
                 VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)""", (
        idn,
        d.get("violation_date") or datetime.utcnow().strftime("%Y-%m-%d %H:%M:%S"),
        d.get("category") or None,
        d.get("severity") or None,
        d.get("description") or None,
        d.get("action_taken") or None,
        d.get("officer") or None,
        bool(d.get("court_notified")),
        bool(d.get("da_notified")),
    ))
    conn.commit()
    _cache.clear()
    return {"ok": True}


# ───────────────────────────────────────────────────────── PINNED DEFENDANTS ───

def list_pinned(user_id: str) -> list[dict]:
    c = get_conn().cursor()
    c.execute("""
        SELECT p.idn, d.defendant_name, d.case_status, d.supervising_officer,
               d.pretrial_level, p.created_at
        FROM dbo.pinned_defendants p
        LEFT JOIN dbo.defendants d ON d.idn = p.idn
        WHERE p.user_id = %s
        ORDER BY p.created_at DESC
    """, (user_id,))
    return [{
        "idn":     str(r[0]),
        "name":    r[1] or "",
        "status":  r[2] or "",
        "officer": _fmt_officer(r[3]) or "",
        "level":   r[4],
        "pinned":  _to_iso(r[5]),
    } for r in c.fetchall()]


def is_pinned(user_id: str, idn: int) -> bool:
    c = get_conn().cursor()
    c.execute("SELECT 1 FROM dbo.pinned_defendants WHERE user_id=%s AND idn=%s",
              (user_id, int(idn)))
    return c.fetchone() is not None


def toggle_pin(user_id: str, idn: int) -> dict:
    if not defendant_exists(int(idn)):
        return {"ok": False, "error": "Defendant not found"}
    conn = get_conn(); c = conn.cursor()
    c.execute("SELECT pin_id FROM dbo.pinned_defendants WHERE user_id=%s AND idn=%s",
              (user_id, int(idn)))
    row = c.fetchone()
    if row:
        c.execute("DELETE FROM dbo.pinned_defendants WHERE pin_id=%s", (row[0],))
        conn.commit()
        return {"ok": True, "pinned": False}
    c.execute("INSERT INTO dbo.pinned_defendants (user_id, idn) VALUES (%s, %s)",
              (user_id, int(idn)))
    conn.commit()
    return {"ok": True, "pinned": True}


# ───────────────────────────────────────────────────────── USER PREFERENCES ───

def get_prefs(user_id: str) -> dict:
    c = get_conn().cursor()
    c.execute("SELECT theme, default_landing, prefs_json FROM dbo.user_preferences WHERE user_id=%s",
              (user_id,))
    r = c.fetchone()
    if not r:
        return {"theme": "dark", "default_landing": "/", "prefs": {}}
    extra = {}
    try:
        if r[2]: extra = json.loads(r[2])
    except Exception:
        pass
    return {
        "theme":           r[0] or "dark",
        "default_landing": r[1] or "/",
        "prefs":           extra,
    }


def set_prefs(user_id: str, theme: str = None, default_landing: str = None,
              prefs: dict = None) -> dict:
    cur = get_prefs(user_id)
    if theme is not None:           cur["theme"] = theme
    if default_landing is not None: cur["default_landing"] = default_landing
    if prefs is not None:           cur["prefs"] = prefs
    conn = get_conn(); c = conn.cursor()
    c.execute("""
        IF EXISTS (SELECT 1 FROM dbo.user_preferences WHERE user_id=%s)
            UPDATE dbo.user_preferences
               SET theme=%s, default_landing=%s, prefs_json=%s, updated_at=SYSUTCDATETIME()
             WHERE user_id=%s
        ELSE
            INSERT INTO dbo.user_preferences (user_id, theme, default_landing, prefs_json)
            VALUES (%s, %s, %s, %s)
    """, (
        user_id, cur["theme"], cur["default_landing"], json.dumps(cur["prefs"]),
        user_id,
        user_id, cur["theme"], cur["default_landing"], json.dumps(cur["prefs"]),
    ))
    conn.commit()
    return {"ok": True, **cur}


# ────────────────────────────────────────────────────────────── REMINDERS ───

def list_reminders(idn: int = None, assigned_to: str = None,
                   include_completed: bool = False, limit: int = 50) -> list[dict]:
    c = get_conn().cursor()
    where = []
    params = []
    if idn is not None:
        where.append("r.idn = %s"); params.append(int(idn))
    if assigned_to:
        where.append("r.assigned_to = %s"); params.append(assigned_to)
    if not include_completed:
        where.append("r.completed = 0")
    where_sql = ("WHERE " + " AND ".join(where)) if where else ""
    sql = f"""
        SELECT TOP {int(limit)} r.reminder_id, r.idn, d.defendant_name, r.body,
               r.due_date, r.assigned_to, r.created_by, r.completed, r.completed_at
        FROM dbo.reminders r
        LEFT JOIN dbo.defendants d ON d.idn = r.idn
        {where_sql}
        ORDER BY ISNULL(r.due_date, '9999-12-31'), r.created_at DESC
    """
    c.execute(sql, tuple(params))
    return [{
        "id":          r[0],
        "idn":         (str(r[1]) if r[1] else None),
        "name":        r[2] or "",
        "body":        r[3] or "",
        "due":         _to_iso(r[4]),
        "assigned_to": _fmt_officer(r[5]) or (r[5] or ""),
        "created_by":  _fmt_officer(r[6]) or (r[6] or ""),
        "completed":   bool(r[7]),
        "completed_at": _to_iso(r[8]),
    } for r in c.fetchall()]


def add_reminder(d: dict) -> dict:
    body = (d.get("body") or "").strip()
    if not body:
        return {"ok": False, "error": "Reminder text required"}
    idn = d.get("idn")
    if idn:
        try: idn = int(idn)
        except (TypeError, ValueError): idn = None
    conn = get_conn(); c = conn.cursor()
    c.execute("""INSERT INTO dbo.reminders (idn, body, due_date, assigned_to, created_by)
                 VALUES (%s, %s, %s, %s, %s)""", (
        idn, body, d.get("due_date") or None,
        d.get("assigned_to") or None, d.get("created_by") or None,
    ))
    conn.commit()
    return {"ok": True}


def complete_reminder(reminder_id: int, user: str) -> dict:
    conn = get_conn(); c = conn.cursor()
    c.execute("""UPDATE dbo.reminders
                 SET completed=1, completed_at=SYSUTCDATETIME(), completed_by=%s
                 WHERE reminder_id=%s""", (user, int(reminder_id)))
    conn.commit()
    return {"ok": True}


def delete_reminder(reminder_id: int) -> dict:
    conn = get_conn(); c = conn.cursor()
    c.execute("DELETE FROM dbo.reminders WHERE reminder_id=%s", (int(reminder_id),))
    conn.commit()
    return {"ok": True}


# ────────────────────────────────────────────────── COMPLIANCE / ALERTS ───

def overdue_check_ins(days: int = 14, limit: int = 200) -> list[dict]:
    """Open defendants whose most recent check-in is older than N days
    (or who have no check-in at all). Used for the alerts badge."""
    c = get_conn().cursor()
    c.execute(f"""
        WITH last_ci AS (
          SELECT idn, MAX(TRY_CONVERT(datetime2, check_in_date)) AS last_ci
          FROM dbo.check_ins
          GROUP BY idn
        )
        SELECT TOP {int(limit)} d.idn, d.defendant_name, d.supervising_officer,
               d.pretrial_level, lc.last_ci, d.referral_date
        FROM dbo.defendants d
        LEFT JOIN last_ci lc ON lc.idn = d.idn
        WHERE d.source IN ('blue_book','both')
          AND d.case_status LIKE 'open%%'
          AND (lc.last_ci IS NULL OR lc.last_ci < DATEADD(day, -{int(days)}, GETDATE()))
        ORDER BY lc.last_ci ASC
    """)
    out = []
    for r in c.fetchall():
        out.append({
            "idn":     str(r[0]),
            "name":    r[1] or "",
            "officer": _fmt_officer(r[2]) or "",
            "level":   r[3],
            "last_ci": _to_iso(r[4]),
            "referral": _fmt_date(r[5]) or "",
        })
    return out


def alerts_summary(officer_email: str = None) -> dict:
    """Counts of things that need attention. If officer_email given,
    scoped to that officer's caseload."""
    c = get_conn().cursor()
    where_off = ("AND d.supervising_officer = %s" if officer_email else "")
    params_off = (officer_email,) if officer_email else ()

    # Overdue check-ins (14+ days)
    c.execute(f"""
        WITH last_ci AS (
          SELECT idn, MAX(TRY_CONVERT(datetime2, check_in_date)) AS last_ci
          FROM dbo.check_ins
          GROUP BY idn
        )
        SELECT COUNT(*) FROM dbo.defendants d
        LEFT JOIN last_ci lc ON lc.idn = d.idn
        WHERE d.source IN ('blue_book','both')
          AND d.case_status LIKE 'open%%'
          AND (lc.last_ci IS NULL OR lc.last_ci < DATEADD(day, -14, GETDATE()))
          {where_off}
    """, params_off)
    overdue = c.fetchone()[0]

    # GPS pending DA notification
    c.execute(f"""SELECT COUNT(*) FROM dbo.gps_events g
                 LEFT JOIN dbo.defendants d ON d.idn = g.idn
                 WHERE g.case_status LIKE 'open%%'
                   AND (g.da_emailed IS NULL OR g.da_emailed = 0)
                   {where_off}""", params_off)
    gps_pending = c.fetchone()[0]

    # Open violations not yet acted on
    c.execute(f"""SELECT COUNT(*) FROM dbo.violations v
                 LEFT JOIN dbo.defendants d ON d.idn = v.idn
                 WHERE v.court_notified = 0 AND v.da_notified = 0
                   {where_off}""", params_off)
    pending_violations = c.fetchone()[0]

    # Court dates this week
    c.execute(f"""SELECT COUNT(*) FROM dbo.court_dates cd
                 LEFT JOIN dbo.defendants d ON d.idn = cd.idn
                 WHERE cd.court_date >= CAST(GETDATE() AS DATETIME2)
                   AND cd.court_date <= DATEADD(day, 7, GETDATE())
                   {where_off}""", params_off)
    court_this_week = c.fetchone()[0]

    # Open reminders for this user
    if officer_email:
        c.execute("SELECT COUNT(*) FROM dbo.reminders WHERE assigned_to=%s AND completed=0",
                  (officer_email,))
        my_reminders = c.fetchone()[0]
    else:
        c.execute("SELECT COUNT(*) FROM dbo.reminders WHERE completed=0")
        my_reminders = c.fetchone()[0]

    return {
        "overdue_checkins":   overdue,
        "gps_pending_da":     gps_pending,
        "pending_violations": pending_violations,
        "court_this_week":    court_this_week,
        "my_reminders":       my_reminders,
        "total":              overdue + gps_pending + pending_violations + my_reminders,
    }


# ─────────────────────────────────────────────────────────────── MY DAY ───

def my_caseload(officer_email: str, limit: int = 200) -> list[dict]:
    c = get_conn().cursor()
    c.execute(f"""
        WITH last_ci AS (
          SELECT idn, MAX(TRY_CONVERT(datetime2, check_in_date)) AS last_ci
          FROM dbo.check_ins
          GROUP BY idn
        ),
        last_pm AS (
          SELECT idn, MAX(TRY_CONVERT(datetime2, payment_date)) AS last_pm,
                 SUM(payment_amount) AS total_paid
          FROM dbo.payments
          GROUP BY idn
        )
        SELECT TOP {int(limit)}
            d.idn, d.defendant_name, d.case_status, d.pretrial_level,
            d.charge_type, d.gps, d.referral_date,
            lc.last_ci, lp.last_pm, lp.total_paid,
            (SELECT TOP 1 CONCAT('@', case_number) FROM dbo.cases x WHERE x.idn = d.idn) AS case_no
        FROM dbo.defendants d
        LEFT JOIN last_ci lc ON lc.idn = d.idn
        LEFT JOIN last_pm lp ON lp.idn = d.idn
        WHERE d.source IN ('blue_book','both')
          AND d.case_status LIKE 'open%%'
          AND d.supervising_officer = %s
        ORDER BY lc.last_ci ASC
    """, (officer_email,))
    out = []
    for r in c.fetchall():
        out.append({
            "idn":      str(r[0]),
            "name":     r[1] or "",
            "status":   (r[2] or "").title(),
            "level":    r[3],
            "charge":   r[4] or "",
            "gps":      bool(r[5]),
            "referral": _fmt_date(r[6]) or "",
            "last_ci":  _to_iso(r[7]),
            "last_pm":  _to_iso(r[8]),
            "total_paid": _d(r[9]),
            "caseNum":  r[10] or "",
        })
    return out


def my_day_bundle(user_email: str) -> dict:
    """Everything the My Day page needs in one trip."""
    return {
        "officer":   user_email,
        "alerts":    alerts_summary(user_email),
        "caseload":  my_caseload(user_email),
        "reminders": list_reminders(assigned_to=user_email),
        "court_this_week": [
            cd for cd in upcoming_court_dates(days=7)
            if cd.get("officer") and cd["officer"].lower() in (user_email or "").lower()
        ],
        "pinned":    list_pinned(user_email),
    }


# ───────────────────────────────────────────────────── DEFENDANT TIMELINE ───

def defendant_timeline(idn: int, limit: int = 100) -> list[dict]:
    """Unified chronological history: check-ins, payments, GPS, notes, edits."""
    c = get_conn().cursor()
    items = []

    c.execute("""SELECT check_in_date, type_of_check_in, supervising_officer
                 FROM dbo.check_ins WHERE idn=%s
                 ORDER BY TRY_CONVERT(datetime2, check_in_date) DESC""", (int(idn),))
    for r in c.fetchall():
        items.append({
            "kind": "check-in", "ts": _to_iso(r[0]),
            "title": "Check-in",
            "desc": f"{r[1] or 'Check-in'} · {_fmt_officer(r[2]) or 'officer'}",
        })
    c.execute("""SELECT payment_date, payment_amount, payment_type, officer
                 FROM dbo.payments WHERE idn=%s
                 ORDER BY TRY_CONVERT(datetime2, payment_date) DESC""", (int(idn),))
    for r in c.fetchall():
        items.append({
            "kind": "payment", "ts": _to_iso(r[0]),
            "title": f"Payment ${_d(r[1]):,.2f}",
            "desc": f"{r[2] or 'Payment'} · {_fmt_officer(r[3]) or (r[3] or 'officer')}",
        })
    c.execute("""SELECT gps_install_date, gps_type, case_status FROM dbo.gps_events WHERE idn=%s""",
              (int(idn),))
    for r in c.fetchall():
        items.append({
            "kind": "gps", "ts": _to_iso(r[0]),
            "title": "GPS installed",
            "desc": f"{r[1] or 'Monitor'} · {r[2] or 'Open'}",
        })
    c.execute("""SELECT created_at, body, author FROM dbo.defendant_notes WHERE idn=%s""",
              (int(idn),))
    for r in c.fetchall():
        items.append({
            "kind": "note", "ts": _to_iso(r[0]),
            "title": "Note",
            "desc": (r[1][:120] + ('…' if len(r[1] or '') > 120 else '')) +
                    (f' · {_fmt_officer(r[2]) or r[2]}' if r[2] else ''),
        })
    c.execute("""SELECT violation_date, category, severity, description, officer
                 FROM dbo.violations WHERE idn=%s""", (int(idn),))
    for r in c.fetchall():
        items.append({
            "kind": "violation", "ts": _to_iso(r[0]),
            "title": f"Violation · {r[1] or '—'} ({r[2] or '—'})",
            "desc": (r[3] or '')[:160],
        })
    c.execute("""SELECT court_date, court, notes FROM dbo.court_dates WHERE idn=%s""",
              (int(idn),))
    for r in c.fetchall():
        items.append({
            "kind": "court", "ts": _to_iso(r[0]),
            "title": f"Court · {r[1] or 'TBD'}",
            "desc": r[2] or '',
        })
    c.execute(f"""SELECT TOP {int(limit)} ts, user_id, col_name, old_value, new_value
                 FROM dbo.audit_log
                 WHERE table_name='defendants' AND row_id=%s
                 ORDER BY ts DESC""", (str(int(idn)),))
    for r in c.fetchall():
        items.append({
            "kind": "edit", "ts": _to_iso(r[0]),
            "title": f"Edited · {r[2] or 'field'}",
            "desc": f"{(r[3] or '—')[:50]} → {(r[4] or '—')[:50]} by {_fmt_officer(r[1]) or r[1] or 'someone'}",
        })

    items.sort(key=lambda x: x["ts"] or "", reverse=True)
    return items[:int(limit)]


# ──────────────────────────────────────────────────────── SAVED SEARCHES ───

def list_saved_searches(user_id: str) -> list[dict]:
    c = get_conn().cursor()
    c.execute("""SELECT search_id, name, spec, page_path, is_pinned, created_at
                 FROM dbo.saved_searches
                 WHERE user_id=%s ORDER BY is_pinned DESC, created_at DESC""", (user_id,))
    return [{
        "id":      r[0],
        "name":    r[1],
        "spec":    json.loads(r[2]) if r[2] else {},
        "page":    r[3] or "",
        "pinned":  bool(r[4]),
        "when":    _to_iso(r[5]),
    } for r in c.fetchall()]


def add_saved_search(user_id: str, name: str, spec: dict, page: str = None,
                     pinned: bool = False) -> dict:
    if not name: return {"ok": False, "error": "Name required"}
    conn = get_conn(); c = conn.cursor()
    c.execute("""INSERT INTO dbo.saved_searches (user_id, name, spec, page_path, is_pinned)
                 VALUES (%s, %s, %s, %s, %s)""",
              (user_id, name, json.dumps(spec or {}), page, 1 if pinned else 0))
    conn.commit()
    return {"ok": True}


def delete_saved_search(search_id: int, user_id: str) -> dict:
    conn = get_conn(); c = conn.cursor()
    c.execute("DELETE FROM dbo.saved_searches WHERE search_id=%s AND user_id=%s",
              (int(search_id), user_id))
    conn.commit()
    return {"ok": True}
