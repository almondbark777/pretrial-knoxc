# Phase 2 — Parity Audit & Live-Drift Matrix

> **Phase 2 tracking doc** (see Brief Part 0.1 / Part 3). Append-only.
> Read `PHASE_0_INVENTORY.md` and `PHASE_1_HEALTH.md` before this doc.

---

## Entry — 2026-05-30 · Model: claude-opus-4-8 (1M context) · Effort: high (autonomous)

### What was audited

The current Python webapp on `ptr1` vs the canonical business-rules spec
(PTR Client Lookup **v0.82 (SQL-connected)**, data layer
`assets/8a6913e5-7c4a-4b1b-ad95-28756bd8bf2a.js`). Per Brief Part 0.2 (LOCKED),
the parity target is the full Part 3.2 matrix, plus the new **2.7 live-drift
audit** of the server-side compliance queries.

### Method

- Read the canonical data layer JS (`computeCheckIns`, `computePTRFees`,
  `computeGPS`, `parsePretrialLevel`, `_parseDay`, `colFind`, `buildClients`).
- Read the live Python path (`app_lookup.py` → `queries_ext.lookup_datasets()`),
  the legacy/server-side query layer (`queries.py`, `queries_ext.py`), the
  importer (`sharepoint_import.py`), and the T-SQL→SQLite shim (`sqlite_compat.py`).
- Wrote a **faithful Python port** of the canonical JS data layer at
  [`tools/parity_ref.py`](tools/parity_ref.py), ran it against the offline DB
  copy `db/kh222.db`, and **hand-verified** the outputs. This port doubles as
  the executable reference for the Phase 4 Go rewrite (golden values below).
- Data facts confirmed directly against `db/kh222.db` (offline copy; row counts
  lag `ptr1` per Phase 1, but schema + value shapes are representative).

> ⚠ **Scope limit:** this session ran on the Windows box (`rzr`), not on `ptr1`.
> Items marked **[verify on ptr1]** could not be checked against the live DB and
> are listed as follow-up actions, not confirmed facts.

---

## 0. The decisive architecture fact (changes how to read this matrix)

**The live lookup HTML is byte-identical to the canonical spec.**

```
md5  fda9d14ae732fc65ac5b385542f47f6e  webapp/lookup/PTR_Client_Lookup.html
md5  fda9d14ae732fc65ac5b385542f47f6e  PTR Client Lookup v0.82 (SQL-connected).html
```

The live app (`app_lookup.py`) serves that HTML at `/` and feeds it raw rows at
`/api/lookup_data` (`queries_ext.lookup_datasets()`). The HTML runs the
**canonical JS business math client-side** (the `assets/` files are its decoded
gzip+base64 bundle). 

**Consequence:** for the *live feature*, every "business-math" row of the Part 3.2
matrix is **GREEN by construction** — the webapp ships the spec unchanged. There
is no possible logic drift in `computeCheckIns/PTRFees/GPS` for the live tool.
The only place a regression can hide for the live feature is the **SQL → JS
data-feed boundary**: column mapping, missing columns, and value coercion
(`queries_ext._BB_MAP/_CI_MAP/_PM_MAP/_GP_MAP` + `_ls_str`). That boundary is the
focus of §1.

The *server-side* reimplementations in `queries.py`/`queries_ext.py`
(`overdue_check_ins`, `alerts_summary`, `my_caseload`, `dashboard_stats`,
`analytics_bundle`, `recent_activity`) are **not exposed by `app_lookup.py`** —
they belong to the legacy `app.py`, which is **not running** (Phase 1 confirmed
`ExecStart` = `app_lookup:app`). They are audited in §3 (the 2.7 live-drift
audit) because (a) they are latent bugs if `app.py` is ever used and (b) the Go
rewrite **will** expose server-side math and must not copy their logic.

---

## 1. Data-feed boundary audit (the only live-regression surface)

`lookup_datasets()` selects `SELECT * FROM raw_*` and renames snake_case columns
to the exact SharePoint headers `colFind` expects, coercing every value to a
string via `_ls_str` (mimics PapaParse). Verified each mapping:

| Concern | Result | Evidence |
|---|---|---|
| **Check-in date ≠ referral date (Appendix B #7)** | ✅ **GREEN** | `raw_check_ins` has *separate* `date` and `referral_date` columns. `_CI_MAP` maps `date→"Check in Date"` and `referral_date→"Referral Date"` ([queries_ext.py:63](webapp/queries_ext.py:63)). JS `ciDate` colFind = `['Check in Date',…]` with `{exclude:/referral/i}` ([8a6913e5:89](assets/8a6913e5-7c4a-4b1b-ad95-28756bd8bf2a.js:89)). Data check: IDN **1374859 (LUTTRELL)** `date`=`4/27/2026 13:16` while `referral_date`=`4/27/2025 20:00` — the check-in date is correct, **not** stamped with the referral. |
| Importer maps the right SharePoint col → `date` | ✅ GREEN | `DATASETS["checkins"]["date"] = ["Date","Check in Date"]`, exact-normalized match first ([sharepoint_import.py:65](webapp/sharepoint_import.py:65), `_match_headers`:120). |
| `gps` boolean coercion | ✅ GREEN | raw value `'True'`/`'False'` → `_ls_str` → `"True"`; JS `['true','yes','1'].includes('true')` → active. `!!gpRec` is the fallback. |
| `day_adjustment` | ✅ GREEN | `_BB_MAP` maps it; raw stores `'15'` etc.; JS `parseFloat(...)||0`. (Live path keeps the string, so fractional adjustments survive — unlike the legacy bundle, see Y6.) |
| `"order"` reserved word | ✅ GREEN | `_GP_MAP` maps `order→"Order"`; JS `gpOrder` colFind `['Order','Order0']`. |
| **Switched To / Switched GPS Date / Notes columns** | 🔴 **RED (R1)** | `_GP_MAP` maps `switched_to`/`switched_gps_date`/`notes` ([queries_ext.py:98-100](webapp/queries_ext.py:98)) **but `_ls_rows` only emits a header if `snake in r`** ([queries_ext.py:120](webapp/queries_ext.py:120)). `raw_gps_48_hours` in `db/kh222.db` **has none of these columns** (confirmed: schema = `…gps_install_date, order, da_emailed, closed_date`). When absent, `colFind→null` → **switch-aware billing, GPS-relief freeze, and the fee-waiver banner all silently turn off.** The CSV tool read these from the GPS CSV; the SQL feed drops them. **[verify on ptr1]** whether the GPS-48-Hours SharePoint export carries these columns (the importer *will* create them only if the CSV header matches). |

**Net:** the live data feed is correct **except** the GPS switch/relief/waiver
columns (R1), which depend on the upstream SharePoint list including them.

---

## 2. Part 3.2 parity checklist (HTML spec → current state)

Status legend: ✅ live tool runs canonical JS (green by construction);
🟡 latent/server-side only (not live); 🔴 real gap. "Go" column = what the
Phase 4 rewrite must replicate (golden values in §4).

| # | Behaviour | Live (JS) | Evidence / note |
|---|---|---|---|
| 1 | Pretrial level parse (1/L1/I→1 …) | ✅ | `parsePretrialLevel` [8a6913e5:378]. Real data only ever `'1'/'2'/'3'` (dist: 985×3, 766×1, 455×2). |
| 2 | Initial 3-day grace `[ref, ref+3]` | ✅ | [8a6913e5:439]. Sample JONES (1704942): `4/29→5/02`. |
| 3 | L2 monthly windows, anchor = month after initial deadline | ✅ | [8a6913e5:467-497]. REASONOVER (1426070) ref 1/1 → Feb,Mar,Apr,May windows. |
| 4 | L3/GPS weekly Mon–Fri, first = Monday after initial-deadline week | ✅ | [8a6913e5:499-524]. HANCOCK (1704989) ref 4/30 → first week Mon 5/4. |
| 5 | "Future window" guard (don't flag current/upcoming) | ✅ | `isFuture`+`monthClosed`/`weekClosed` [8a6913e5:480-488,508-516]. REASONOVER May window miss=0 (current month). |
| 6 | L1 excluded from monthly Missed roster | ✅ (tool) / 🔴 (server) | Tool: `MissedCheckInsRoster` excludes L1. **Server `overdue_check_ins` does NOT exclude L1** → see Y1. |
| 7 | PTR fee = $20 × months touched (inclusive both ends) | ✅ | [8a6913e5:571-585]. REASONOVER Jan→May = 5×$20 = **$100** (hand-checked). |
| 8 | L1 flat $20 | ✅ | [8a6913e5:563-565]. JONES owes exactly $20. |
| 9 | PTR payment filter `/\bptr\b/` (not `LIKE '%PTR%'`) | ✅ | [8a6913e5:552]. **No `LIKE '%PTR%'` exists anywhere in the Python** (grep clean) — so the TRANSPORT/SUPTRA bug is absent. Go must use word-boundary. |
| 10 | GPS rates SCRAM 15 / ALLIED 8 / IC 0 | ✅ | `_rateOf` [8a6913e5:232]. AGUILAR SCRAM, PIETY ALLIED below. |
| 11 | GPS days = end−start **+1** inclusive | ✅ | [8a6913e5:297]. AGUILAR install 4/20→5/30 = 40+1 = **41 days**. |
| 12 | Switch-aware billing (before×old + $23 + after×new) | 🔴 (R1) | Logic correct in JS [8a6913e5:301-318] (synthetic test: 14×15+23+16×8 = **$361**), but **off when switch columns absent** (R1). |
| 13 | GPS RELIEVED freezes window at switch date (v0.80) | 🔴 (R1) | JS cap uses `_reliefSwitchD < endDate` (strict `<` ✓) [8a6913e5:292-295]. Synthetic relief on day 10 → **$150** (10×15). Off when columns absent (R1). |
| 14 | `_isReliefSwitch` matches no gps / gps relieved / off gps / gps off / removed | 🔴 (R1) | Regex [8a6913e5:240] verified for all 5 phrases. Off when `switched_to` absent (R1). |
| 15 | Day adjustment (BB col + session override) at post-switch rate | ✅ | `_adjRate` = post-switch when `hasSwitch` [8a6913e5:321]. HOLDER (1615103) `day_adjustment='15'`. |
| 16 | Surplus$ = (paid + adj$) − owed | ✅ | [8a6913e5:331]. Synthetic: 500 + 2×15 − 465 = **$65**. |
| 17 | Surplus days = ±ceil(\|$\|/rate), negative uses `-ceil` not `floor` | ✅ | [8a6913e5:335-337]. Synthetic surplus → **5 days**; AGUILAR −615 → **−41 days**. |
| 18 | Fee-waiver banner (`/waiv/i` AND `/(fee\|gps\|payment\|charge)/i`) | 🔴 (R1) | `isFeesWaived` [8a6913e5:655] correct; needs the `notes` column (R1). |
| 19 | Missing-critical-info badges (Level, RefDate, GPS Type, Install Date) | ✅ | Rendered client-side from the fed values; `''`/absent → MISSING. Live feed passes `''` for blank (not the SQL-NULL trap, because `_ls_str(None)=''`). |
| 20 | BehindRoster = every client surplus<0, A–Z | ✅ | Tool computes from fed data. |
| 21 | MissedCheckInsRoster (open, current month, L1 excluded, grace honored) | ✅ (tool) | Tool correct. (Server analogue drifts — Y1.) |
| 22 | Calendar event sources (referral/install/switch/closed/CI split/pay split/missed/due) | ✅ | `getEventsForClient` [8a6913e5:590]. switch event needs R1 cols. |
| 23 | Case-token split on `/[,\s]+/` | ✅ (tool) / 🟡 (write) | Tool tokenizes `Warrant/Case #` correctly. **Legacy write paths use `.split(",")`** → Y2. ETL `cases` uses `re.split(r"[,;\s]+")` ✓ ([build_db.py:161](db/build_db.py:161)). |
| 24 | IDN as primary join key | ✅ | All four `_*_MAP`s key `idn→IDN`; raw tables keyed by `idn`. |
| 25 | EST "today" | ✅ (live) | JS default uses `Intl.DateTimeFormat('America/New_York')` [8a6913e5:281]. Server not involved for live. **Go must compute "today" in America/New_York** (Y3 for server path). |
| 26 | Closed cases stop accruing windows + fee months | ✅ | `effEnd = min(track, closed)` [8a6913e5:432,561]. COLLINS (1704603) ref=closed=4/26 → only initial window, miss=0, 1 fee month. |

---

## 3. The 2.7 live-drift audit (server-side queries vs the canonical math)

These power the legacy `app.py` only (not live), but document the drift so the
Go rewrite reimplements the **windows logic**, not these shortcuts.

| Server query | Drift from spec | Severity |
|---|---|---|
| `overdue_check_ins` / `alerts_summary` ([queries_ext.py:546,578]) | Defines "overdue" as **>14 days since last check-in**, level-agnostic. Canonical: L2 = per calendar month, L3/GPS = per Mon–Fri week, L1 = initial only. → **under-reports** an L3 who missed a week but checked in 10 days ago; **over-reports** an L1 who never re-checks-in (spec excludes L1). Also reads normalized `check_ins` which **lags raw 2,600 vs 5,000** (Phase 1) → stale. | 🟡 Y1 (latent; not live) |
| `recent_activity`, `dashboard_stats`, `analytics_bundle` ([queries.py:127,219,266]) | Read normalized `check_ins`/`payments`/`gps_events`, which **lag the raw tables** (Phase 1: payments 1,947 vs 2,826; gps 337 vs 777). Counts/sums understate reality. | 🟡 Y2 (ETL lag; not live) |
| `dashboard_stats` "today"/month boundary | `datetime.utcnow()` + `GETDATE()` (server local/UTC), not America/New_York. A late-night ET request can bucket into the wrong month. | 🟡 Y3 (not live) |
| Write paths `insert_referral` / `update_defendant` ([queries.py:509,740]) | Case numbers split with **`.split(",")`** — breaks space-joined `"@A @B"` (Appendix B **#3**). Real data has `"@1656416 & @1656418"` (IDN 1267951). | 🟡 Y2 (latent; not live) |

---

## 4. Sample clients — ground-truth values (canonical math, trackDate 2026-05-30)

Computed by [`tools/parity_ref.py`](tools/parity_ref.py) (faithful JS port),
hand-verified. **These are the golden values the Go rewrite must reproduce.**

| IDN | Name | Lvl | Ref / Closed | Check-ins (win/missed) | PTR (months · owed) | GPS (vendor·rate·days·owed·surplus$/days) |
|---|---|---|---|---|---|---|
| 1704942 | JONES | 1 | 4/29/26 | 1 / 1 (initial missed) | L1 flat · **$20** | — |
| 1426070 | REASONOVER | 2 | 1/1/26 | 5 / 3 | 5 · **$100** | — |
| 1704989 | HANCOCK | 3 | 4/30/26 | 5 / 5 | 2 · **$40** | — |
| 1704603 | COLLINS | 2 | 4/26/26 (closed 4/26) | 1 / 0 | 1 · **$20** | — |
| 1386687 | AGUILAR | 3(GPS) | 4/17/26 | 6 / 5 | 2 · $40 | SCRAM·15·**41**·**$615**·−615/**−41** |
| 1340291 | PIETY | 3(GPS) | 4/27/26 | 5 / 4 | 2 · $40 | ALLIED·8·**33**·**$264**·−264/−33 |
| 1374859 | LUTTRELL | 3(GPS) | 4/27/25 | 57 / 49 | 14 · $280 (paid $40) | SCRAM·15·**418**·**$6270**·−460/−31 (paid $5810) |

Synthetic switch/relief validation (no live data exercises these):
`SCRAM 1/1→1/31` = $465; `switch→ALLIED 1/15` = $361; `relief "no gps" 1/10` =
$150 (capped); `IC` rate = $0; `paid 500 + adj 2d` → surplus **$65 / 5 days**.

---

## 5. Findings rolled up for Phase 3

| ID | Severity | What | Live impact | Fix owner |
|---|---|---|---|---|
| **R1** | 🔴 RED | GPS `switched_to`/`switched_gps_date`/`notes` absent from `raw_gps_48_hours` → switch billing, relief-freeze, waiver banner OFF in SQL-fed tool | **Yes** (GPS clients who switched/were relieved bill wrong vs CSV tool) | Code already maps them; **[verify on ptr1]** the GPS SharePoint export includes the columns. If not, add to the list/flow. Code-side: make `_ls_rows` emit mapped headers as `""` even when the column is absent, so `colFind` discovery is stable. |
| **R2** | 🔴 RED (write) | `insert_referral`/`update_defendant` split cases on `","` only (Appendix B #3) | No (legacy endpoints) | `re.split(r"[,\s]+")`. Cheap, correct, prevents future regression. |
| Y1 | 🟡 | `overdue_check_ins`/`alerts_summary` use flat 14-day rule, ignore level, don't exclude L1, read stale normalized table | No (legacy) | Don't port to Go; Go implements windows. (Optional: fix Python if `app.py` is ever revived.) |
| Y2 | 🟡 | Normalized tables lag raw; write-path comma split | No (legacy) | ETL/Go concern. |
| Y3 | 🟡 | `_vendorOf` `.includes('IC')` could false-match (e.g. "INDICATED"); real data "IN CUSTODY" does **not** contain "IC" so it does not fire today | No | Advisory; Go may tighten to word-ish match but must stay `includes`-compatible with spec. |
| Y4 | 🟡 | `_fmt_date`/`sqlite_compat` date parsing is naive (no tz anchor) | No (display/legacy) | Go: one tz-aware noon-anchored parser. |

**RED items to action in Phase 3: R1 (verify + harden), R2 (fix).**
Everything else is GREEN (live) or latent-for-Go.

### Next step
Proceed to **Phase 3 — Fixes** (`PHASE_3_FIXES.md`): action R1/R2, then **Phase 4
— Go rewrite** (`PHASE_4_GO_REWRITE.md`), using `tools/parity_ref.py` §4 golden
values as the acceptance test.
