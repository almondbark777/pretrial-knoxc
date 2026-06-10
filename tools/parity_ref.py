#!/usr/bin/env python3
"""
parity_ref.py — faithful Python port of the canonical PTR Client Lookup
JS data layer (assets/8a6913e5-*.js): parsePretrialLevel, _parseDay,
computeCheckIns, computePTRFees, computeGPS.

Tracks tracker v0.83 / Go compute (2026-06-02 phone-vs-in-person rule): a
check-in window is satisfied only when BOTH an in-person and a phone contact
occur in it; _ci_kind() mirrors compute.CheckInKind / the bundle's _ciKind
byte-for-byte. (v0.82 counted any check-in type — goldens generated before
2026-06-02 used that older rule.)

Purpose (Phase 2 parity audit + Phase 4 Go seed):
  * The LIVE webapp (app_lookup.py -> /api/lookup_data -> lookup_datasets())
    feeds raw_* rows to the EMBEDDED JS, which runs this exact math in the
    browser. So for the live feature, parity is automatic. This port lets us
    (a) produce concrete ground-truth numbers for the parity matrix, and
    (b) hand a line-for-line reference to the Go rewrite, which re-implements
    the math server-side.

Reads the SQLite DB the same way lookup_datasets() remaps it, so the inputs
match what the JS sees.  Run:
    python tools/parity_ref.py db/kh222.db <IDN>
    python tools/parity_ref.py db/kh222.db --sample
"""
from __future__ import annotations

import math
import re
import sqlite3
import sys
from datetime import datetime, timezone, timedelta

# ── date helpers (mirror JS Date.UTC(y,m-1,d,12,0,0) noon-UTC normalization) ──

NOON = dict(hour=12, minute=0, second=0, microsecond=0)


def _mkdate(y, mo, d):
    return datetime(y, mo, d, 12, 0, 0, tzinfo=timezone.utc)


_re_iso = re.compile(r"^(\d{4})-(\d{1,2})-(\d{1,2})")
_re_us = re.compile(r"^(\d{1,2})/(\d{1,2})/(\d{4})")


def parse_day(s):
    """Port of _parseDayImpl. Returns tz-aware datetime at noon UTC, or None."""
    if not s:
        return None
    t = str(s).strip()
    if not t:
        return None
    m = _re_iso.match(t)
    if m:
        y, mo, d = int(m.group(1)), int(m.group(2)), int(m.group(3))
    else:
        m = _re_us.match(t)
        if m:
            mo, d, y = int(m.group(1)), int(m.group(2)), int(m.group(3))
        else:
            y = mo = d = 0
    if y and mo and d:
        try:
            return _mkdate(y, mo, d)
        except ValueError:
            return None
    # Fallback: try a handful of full-string formats (JS uses new Date(t) +
    # America/New_York). Rare for this data; the regexes catch the real formats.
    for fmt in ("%Y-%m-%dT%H:%M:%S", "%Y-%m-%d %H:%M:%S", "%m/%d/%Y %H:%M",
                "%m/%d/%Y", "%Y-%m-%d"):
        try:
            dt = datetime.strptime(t, fmt)
            return _mkdate(dt.year, dt.month, dt.day)
        except ValueError:
            continue
    return None


def add_days(d, n):
    return d + timedelta(days=n)


def monday_of_week(d):
    # JS: 0=Sun..6=Sat; shift back so Monday=0. Python weekday(): Mon=0..Sun=6.
    dow = d.weekday()  # already Monday=0
    return add_days(d, -dow)


def first_of_month(d):
    return _mkdate(d.year, d.month, 1)


def last_of_month(d):
    if d.month == 12:
        nxt = _mkdate(d.year + 1, 1, 1)
    else:
        nxt = _mkdate(d.year, d.month + 1, 1)
    return add_days(nxt, -1)


def next_month(d):
    if d.month == 12:
        return _mkdate(d.year + 1, 1, 1)
    return _mkdate(d.year, d.month + 1, 1)


def days_between(a, b):
    # JS Math.round((b - a)/86400000). noon-UTC => exact integer days.
    return round((b - a).total_seconds() / 86400.0)


# ── level parsing ──

def parse_level(raw):
    if not raw:
        return None
    s = str(raw).strip().upper()
    if not s:
        return None
    if re.match(r"^(L?1|LEVEL\s*1|LEVEL\s*ONE|I)$", s):
        return 1
    if re.match(r"^(L?2|LEVEL\s*2|LEVEL\s*TWO|II)$", s):
        return 2
    if re.match(r"^(L?3|LEVEL\s*3|LEVEL\s*THREE|III)$", s):
        return 3
    m = re.search(r"(\d)", s)
    return int(m.group(1)) if m else None


# ── check-in compliance ──

_re_letters = re.compile(r"[^a-z]")


def _ci_kind(typ):
    """Mirror of Go compute.CheckInKind / tracker v0.83 _ciKind: classify a
    check-in type into (in_person, phone). Unknown/junk satisfies neither."""
    n = _re_letters.sub("", str(typ or "").lower())
    if "inperson" in n or "office" in n or "walkin" in n:
        return True, False
    if any(k in n for k in ("phone", "text", "call", "virtual", "video", "tele")):
        return False, True
    return False, False


def compute_check_ins(c, today_str=None):
    level = c["_level"]
    refdate = c["_refD"]
    today = parse_day(today_str) if today_str else c["_today"]
    if not refdate:
        return {"level": level, "refDate": None, "today": today, "windows": [],
                "missed": [], "lastCheckIn": None, "lastInPerson": None,
                "lastPhone": None, "nextDue": None, "error": "No referral date"}
    closed = c["_closedD"]
    eff_end = closed if (closed and closed < today) else today

    # v0.83 rule: each window needs BOTH an in-person and a phone contact.
    in_person_ci, phone_ci, all_ci = [], [], []
    for ci in c["checkIns"]:
        d = ci["_d"]
        if not d:
            continue
        all_ci.append(d)
        ip, ph = _ci_kind(ci.get("type"))
        if ip:
            in_person_ci.append(d)
        if ph:
            phone_ci.append(d)
    all_ci.sort()
    in_person_ci.sort()
    phone_ci.sort()
    last_ci = all_ci[-1] if all_ci else None
    last_in_person = in_person_ci[-1] if in_person_ci else None
    last_phone = phone_ci[-1] if phone_ci else None

    def _hit(dates, start, end):
        return any(start <= d <= end for d in dates)

    initial_deadline = add_days(refdate, 3)
    init_ip = _hit(in_person_ci, refdate, initial_deadline)
    init_ph = _hit(phone_ci, refdate, initial_deadline)
    initial_made = init_ip and init_ph
    initial_missed = (not initial_made) and eff_end > initial_deadline

    windows = [{
        "type": "initial", "start": refdate, "end": initial_deadline,
        "deadline": initial_deadline, "satisfied": initial_made,
        "satisfiedInPerson": init_ip, "satisfiedPhone": init_ph,
        "missed": initial_missed, "label": "Initial (3-day)",
    }]

    common = {"refDate": refdate, "today": eff_end, "lastCheckIn": last_ci,
              "lastInPerson": last_in_person, "lastPhone": last_phone}

    if level == 1:
        return {"level": level, "windows": windows,
                "missed": [w for w in windows if w["missed"]],
                "nextDue": None if initial_made else {"type": "initial"}, **common}

    if level == 2:
        cur = next_month(first_of_month(initial_deadline))
        while cur <= eff_end:
            month_end = last_of_month(cur)
            window_end = month_end if month_end < eff_end else eff_end
            ip = _hit(in_person_ci, cur, window_end)
            ph = _hit(phone_ci, cur, window_end)
            hit = ip and ph
            month_closed = eff_end >= month_end or (closed and closed <= month_end)
            is_future = cur > eff_end
            windows.append({
                "type": "month", "start": cur, "end": month_end, "deadline": month_end,
                "satisfied": hit, "satisfiedInPerson": ip, "satisfiedPhone": ph,
                "missed": (not hit and bool(month_closed) and not is_future),
                "label": cur.strftime("%B %Y"),
            })
            cur = next_month(cur)
        missed = [w for w in windows if w["missed"]]
        return {"level": level, "windows": windows, "missed": missed,
                "nextDue": _next_due(windows, eff_end), **common}

    # Level 3 (or anything not 1/2 — incl GPS-as-L3 and unknown). Mon-Fri weeks.
    week_mon = add_days(monday_of_week(initial_deadline), 7)
    guard = 0
    while week_mon <= eff_end and guard < 400:
        guard += 1
        week_fri = add_days(week_mon, 4)
        window_end = week_fri if week_fri < eff_end else eff_end
        ip = _hit(in_person_ci, week_mon, window_end)
        ph = _hit(phone_ci, week_mon, window_end)
        hit = ip and ph
        week_closed = eff_end >= week_fri
        is_future = week_mon > eff_end
        windows.append({
            "type": "week", "start": week_mon, "end": week_fri, "deadline": week_fri,
            "satisfied": hit, "satisfiedInPerson": ip, "satisfiedPhone": ph,
            "missed": (not hit and week_closed and not is_future),
            "label": "Week of " + week_mon.strftime("%b %d"),
        })
        week_mon = add_days(week_mon, 7)
    missed = [w for w in windows if w["missed"]]
    out_level = level if level else (3 if c["gpsActive"] else None)
    return {"level": out_level, "windows": windows, "missed": missed,
            "nextDue": _next_due(windows, eff_end), **common}


def _next_due(windows, eff_end):
    for w in windows:
        if not w["satisfied"] and not w["missed"] and w["start"] <= eff_end:
            return w
    for w in windows:
        if not w["satisfied"] and w["start"] > eff_end:
            return w
    return None


# ── PTR fees ──

def compute_ptr_fees(c, today_str=None):
    level = c["_level"]
    refdate = c["_refD"]
    today = parse_day(today_str) if today_str else c["_today"]
    gps_pay_types = ("gps", "allied", "scram")

    ptr_pays = [p for p in c["payments"]
                if re.search(r"\bptr\b", (p["type"] or "").strip().lower())]
    total_paid = sum(p["amt"] for p in ptr_pays)

    if not refdate or not today:
        return {"level": level, "monthsOwed": [], "totalOwed": 0, "totalPaid": total_paid,
                "balance": total_paid, "applies": False}
    closed = c["_closedD"]
    eff_end = closed if (closed and closed < today) else today

    if level == 1:
        return {"level": level, "monthsOwed": [{"label": "One-time L1 fee", "amount": 20}],
                "totalOwed": 20, "totalPaid": total_paid, "balance": total_paid - 20, "applies": True}
    if level != 2 and level != 3 and not c["gpsActive"]:
        return {"level": level, "monthsOwed": [], "totalOwed": 0, "totalPaid": total_paid,
                "balance": total_paid, "applies": False}

    months = []
    cur = first_of_month(refdate)
    end_cur = first_of_month(eff_end)
    guard = 0
    while cur <= end_cur and guard < 600:
        guard += 1
        months.append({"label": cur.strftime("%b %Y"), "amount": 20})
        cur = next_month(cur)
    total_owed = len(months) * 20
    return {"level": level, "monthsOwed": months, "totalOwed": total_owed, "totalPaid": total_paid,
            "balance": total_paid - total_owed, "applies": True}


# ── GPS billing ──

def _vendor_of(t):
    u = (t or "").upper()
    if "SCRAM" in u:
        return "SCRAM"
    if "ALLIED" in u:
        return "ALLIED"
    if "IC" in u:
        return "IC"
    return ""


def _rate_of(v):
    return {"SCRAM": 15, "ALLIED": 8, "IC": 0}.get(v, None)


_relief_re = re.compile(r"\bno\s*gps\b|\bgps\s*reliev|\boff\s*gps\b|\bgps\s*off\b|\bremov")


def _is_relief_switch(t):
    u = (t or "").strip().lower()
    return bool(u) and bool(_relief_re.search(u))


def compute_gps(c, today_str=None, session_adj=None):
    gps_type_raw = (c["gpsType"] or "").upper()
    vendor = _vendor_of(gps_type_raw)
    daily_rate = _rate_of(vendor)
    vendor2 = _vendor_of(c["gpSwitchedTo"] or "")
    daily_rate2 = _rate_of(vendor2)

    gps_pay_types = ("gps", "allied", "scram")
    gps_payments = [p for p in c["payments"] if (p["type"] or "").strip().lower() in gps_pay_types]
    total_gps_paid = sum(p["amt"] for p in gps_payments)

    bb_adj = c["dayAdj"] or 0
    adj = session_adj if session_adj is not None else bb_adj

    install = c["gpInstall"] or ""
    days_active = None
    start = end = None
    today = parse_day(today_str) if today_str else c["_today"]
    if install:
        start = parse_day(install)
        if start:
            closed = c["_closedD"]
            end = today
            if closed and closed < today:
                end = closed
            relief_d = parse_day(c["gpSwitchedDate"]) if c["gpSwitchedDate"] else None
            if _is_relief_switch(c["gpSwitchedTo"]) and relief_d and start <= relief_d < end:
                end = relief_d
            days_active = max(0, days_between(start, end) + 1)

    switch_d = parse_day(c["gpSwitchedDate"]) if c["gpSwitchedDate"] else None
    has_switch = bool(c["gpSwitchedTo"] and switch_d and daily_rate2 is not None
                      and start and end and start <= switch_d <= end)

    total_owed = None
    if daily_rate is not None and start and end:
        if has_switch:
            d_before = max(0, days_between(start, switch_d))
            d_after = max(0, days_between(switch_d, end))
            total_owed = d_before * daily_rate + 23 + d_after * daily_rate2
        elif days_active is not None:
            total_owed = days_active * daily_rate

    adj_rate = daily_rate2 if (has_switch and daily_rate2 is not None) else daily_rate
    adj_dollars = adj * adj_rate if adj_rate is not None else 0

    surplus_dollars = None
    if total_owed is not None:
        surplus_dollars = (total_gps_paid + adj_dollars) - total_owed

    surplus_days = None
    if surplus_dollars is not None and adj_rate is not None and adj_rate > 0:
        if surplus_dollars >= 0:
            surplus_days = math.ceil(surplus_dollars / adj_rate)
        else:
            surplus_days = -math.ceil(abs(surplus_dollars) / adj_rate)

    return {
        "vendor": vendor, "dailyRate": daily_rate, "vendor2": vendor2, "dailyRate2": daily_rate2,
        "hasSwitch": has_switch, "reliefSwitch": _is_relief_switch(c["gpSwitchedTo"]),
        "totalOwedDollars": total_owed, "totalGpsPaid": total_gps_paid,
        "daysActive": days_active, "adj": adj, "adjDollars": adj_dollars,
        "surplusDollars": surplus_dollars, "surplusDays": surplus_days,
        "covered": (surplus_dollars >= 0) if surplus_dollars is not None else None,
    }


# ── build clients from the SQLite DB (mirrors lookup_datasets + buildClients) ──

def to_num(v):
    s = re.sub(r"[^0-9.-]", "", str(v or "0"))
    try:
        return float(s) if s not in ("", "-", ".", "-.") else 0.0
    except ValueError:
        return 0.0


def build_clients(con, today_str=None):
    con.row_factory = sqlite3.Row
    c = con.cursor()

    def cols(t):
        return {r[1] for r in c.execute(f"PRAGMA table_info({t})")}

    bb_cols = cols("raw_blue_book")
    gp_cols = cols("raw_gps_48_hours")

    # GPS map: one row per idn; row with non-empty install wins.
    gp_map = {}
    for r in c.execute("SELECT * FROM raw_gps_48_hours"):
        r = dict(r)
        k = str(r.get("idn") or "").strip()
        if not k:
            continue
        cur = gp_map.get(k)
        has_install = bool((r.get("gps_install_date") or "").strip())
        cur_has = bool(cur and (cur.get("gps_install_date") or "").strip())
        if not cur or (has_install and not cur_has):
            gp_map[k] = r

    # check-ins / payments grouped by idn
    ci_map = {}
    for r in c.execute("SELECT idn, date, type_of_check_in FROM raw_check_ins"):
        ci_map.setdefault(str(r["idn"] or "").strip(), []).append(
            {"_d": parse_day(r["date"]), "type": r["type_of_check_in"] or ""})
    pm_map = {}
    for r in c.execute("SELECT idn, payment_date, payment_amount, payment_type FROM raw_payments"):
        pm_map.setdefault(str(r["idn"] or "").strip(), []).append(
            {"_d": parse_day(r["payment_date"]), "amt": to_num(r["payment_amount"]),
             "type": r["payment_type"] or ""})

    today = parse_day(today_str) if today_str else _today_est()

    clients = {}
    for r in c.execute("SELECT * FROM raw_blue_book"):
        r = dict(r)
        idn = str(r.get("idn") or "").strip()
        if not idn:
            continue
        gp = gp_map.get(idn)
        gps_raw = str(r.get("gps") or "").lower()
        gps_active = gps_raw in ("true", "yes", "1") or bool(gp)
        cl = {
            "idn": idn,
            "name": (r.get("defendant") or r.get("name") or "").strip(),
            "level": (r.get("pretrial_level") or "").strip(),
            "refdate": (r.get("referral_date") or "").strip(),
            "closed": (r.get("closed_date") or "").strip(),
            "gpsActive": gps_active,
            "gpsType": (r.get("gps_type") or "").strip() or (gp.get("gps_type", "").strip() if gp else ""),
            "dayAdj": to_num(r.get("day_adjustment")),
            "gpInstall": (gp.get("gps_install_date") or "").strip() if gp else "",
            "gpSwitchedTo": (gp.get("switched_to") or "").strip() if (gp and "switched_to" in gp) else "",
            "gpSwitchedDate": (gp.get("switched_gps_date") or "").strip() if (gp and "switched_gps_date" in gp) else "",
            "gpNotes": (gp.get("notes") or "").strip() if (gp and "notes" in gp) else "",
            "checkIns": ci_map.get(idn, []),
            "payments": pm_map.get(idn, []),
        }
        cl["_refD"] = parse_day(cl["refdate"])
        cl["_closedD"] = parse_day(cl["closed"])
        cl["_level"] = parse_level(cl["level"])
        cl["_today"] = today
        # last blue_book row per idn wins (multi-case rows share defendant fields)
        clients[idn] = cl
    return clients


def _today_est():
    # America/New_York "today" at noon UTC. Approx via fixed -4/-5 not needed for
    # audit; use system date. The Go/JS use ET; for parity runs pass --today.
    now = datetime.now(timezone.utc)
    return _mkdate(now.year, now.month, now.day)


def _fmt(d):
    return d.strftime("%Y-%m-%d") if d else None


def dump(c, today_str):
    ci = compute_check_ins(c, today_str)
    ptr = compute_ptr_fees(c, today_str)
    gps = compute_gps(c, today_str)
    print(f"IDN {c['idn']}  {c['name']}")
    print(f"  level={c['level']!r}->{c['_level']}  ref={c['refdate']!r}  closed={c['closed']!r}  "
          f"gpsActive={c['gpsActive']}  gpsType={c['gpsType']!r}  dayAdj={c['dayAdj']}")
    print(f"  #checkIns={len(c['checkIns'])}  #payments={len(c['payments'])}")
    print(f"  CHECK-INS: level={ci['level']} windows={len(ci['windows'])} "
          f"missed={len(ci['missed'])} lastCheckIn={_fmt(ci['lastCheckIn'])} "
          f"lastInPerson={_fmt(ci.get('lastInPerson'))} lastPhone={_fmt(ci.get('lastPhone'))} "
          f"err={ci.get('error')}")
    for w in ci["windows"][:8]:
        print(f"      {w['type']:7} {_fmt(w['start'])}..{_fmt(w['end'])} "
              f"sat={int(w['satisfied'])} ip={int(w.get('satisfiedInPerson', 0))} "
              f"ph={int(w.get('satisfiedPhone', 0))} miss={int(w['missed'])} [{w['label']}]")
    if len(ci["windows"]) > 8:
        print(f"      ... (+{len(ci['windows'])-8} more)")
    print(f"  PTR FEES: applies={ptr['applies']} months={len(ptr['monthsOwed'])} "
          f"owed=${ptr['totalOwed']} paid=${ptr['totalPaid']:.2f} balance=${ptr['balance']:.2f}")
    print(f"  GPS: vendor={gps['vendor']!r} rate={gps['dailyRate']} daysActive={gps['daysActive']} "
          f"owed={gps['totalOwedDollars']} gpsPaid=${gps['totalGpsPaid']:.2f} "
          f"adj={gps['adj']}(${gps['adjDollars']}) surplus$={gps['surplusDollars']} "
          f"surplusDays={gps['surplusDays']}")
    print()


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        return 1
    dbpath = sys.argv[1]
    today_str = None
    args = sys.argv[2:]
    if "--today" in args:
        i = args.index("--today")
        today_str = args[i + 1]
        del args[i:i + 2]
    con = sqlite3.connect(dbpath)
    clients = build_clients(con, today_str)
    if args and args[0] == "--sample":
        ids = args[1:] or []
        for idn in ids:
            if idn in clients:
                dump(clients[idn], today_str)
            else:
                print(f"IDN {idn} not found")
    elif args:
        for idn in args:
            if idn in clients:
                dump(clients[idn], today_str)
            else:
                print(f"IDN {idn} not found")
    return 0


if __name__ == "__main__":
    sys.exit(main())
