# Phase 3 — Fixes

> **Phase 3 tracking doc** (see Brief Part 0.1). Append-only.
> Read `PHASE_2_PARITY_MATRIX.md` first — fixes here action its §5 findings.

---

## Entry — 2026-05-30 · Model: claude-opus-4-8 (1M context) · Effort: high (autonomous)

Phase 2 found the live business math is **green by construction** (the live HTML
is md5-identical to the canonical v0.82 spec). The only real gaps were two RED
items at the data-feed / write boundary. Both actioned below.

---

## Fix R2 — Case-number tokenization in write paths (Appendix B #3)

**Problem.** `insert_referral` and `update_defendant` split case numbers on `","`
only. Grouped cases arrive space-joined (`"@1656416 & @1656418"`, `"@A @B"`), so
`split(",")` collapses them into one malformed token. The canonical JS uses
`split(/[,\s]+/)` everywhere; the ETL (`db/build_db.py:161`) already uses
`re.split(r"[,;\s]+")`. These two write paths were the only offenders.

**Files:** `webapp/queries.py` (`re` already imported at top).

**Diff:**

```diff
@@ insert_referral()  ~L509
     case_number = (d.get("case_number") or "").strip()
     if case_number:
-        for cn in [s.strip().lstrip("@") for s in case_number.split(",") if s.strip()]:
+        # Tokenize on comma OR whitespace — grouped cases arrive as "@A, @B"
+        # AND space-joined as "@A @B" (Appendix B #3). split(",") alone drops
+        # the space-joined form into a single bad token. Mirrors the canonical
+        # JS split(/[,\s]+/) and the ETL split_cases().
+        for cn in [s.strip().lstrip("@") for s in re.split(r"[,\s]+", case_number) if s.strip()]:

@@ update_defendant()  ~L744
     if "case_numbers" in body:
-        new_cases = [s.strip().lstrip("@") for s in (body["case_numbers"] or "").split(",") if s.strip()]
+        # Tokenize on comma OR whitespace (Appendix B #3); mirrors the canonical
+        # JS split(/[,\s]+/). split(",") alone breaks space-joined "@A @B".
+        new_cases = [s.strip().lstrip("@") for s in re.split(r"[,\s]+", body["case_numbers"] or "") if s.strip()]
```

**Before / after** (verified with the real grouped-case value from IDN 1267951,
RHODES, and a space-joined entry):

| Input | Old `split(",")` | New `re.split(r"[,\s]+")` |
|---|---|---|
| `"@A @B"` | `['A @B']` ❌ one bad case | `['A', 'B']` ✅ |
| `"@1656416 & @1656418"` | `['1656416 & @1656418']` ❌ | `['1656416', '&', '1656418']` ✅ (matches canonical JS/ETL exactly; the `&` token is pre-existing spec behavior, harmless for joins) |
| `"@A, @B"` | `['A', 'B']` ✅ | `['A', 'B']` ✅ (unchanged) |
| `"1606962, 1641152"` | `['1606962','1641152']` ✅ | `['1606962','1641152']` ✅ (unchanged) |

**Live impact:** none today (these endpoints belong to the legacy `app.py`, not
the running `app_lookup.py`), but the fix is required by Brief Part 3.1 #3 and
prevents the regression the moment data entry goes live or the Go rewrite reuses
this logic.

---

## Fix R1 — Stable column discovery for the lookup data feed

**Problem.** `lookup_datasets()` only emitted a SharePoint header when the
underlying SQL column existed (`if snake in r`). `raw_gps_48_hours` in the
offline DB has **no** `switched_to` / `switched_gps_date` / `notes` columns, so
those headers never reached the app, `colFind()` returned `null`, and
**switch-aware billing, GPS-relief freezing, and the fee-waiver banner silently
turned off** — features the CSV tool had.

**File:** `webapp/queries_ext.py` (`_ls_rows`).

**Diff:**

```diff
     for r in c.fetchall():
-        row = {}
-        for snake, header in colmap.items():
-            if snake in r:
-                row[header] = _ls_str(r[snake])
+        # Emit EVERY mapped header, even when the underlying SQL column is
+        # absent (-> ""), so the app's colFind() discovers a stable, complete
+        # set of columns regardless of which optional columns this DB happens
+        # to carry. A real CSV always has all its headers; this mirrors that.
+        # (PHASE_2 finding R1.) NOTE: only makes discovery deterministic — the
+        # features still require the source data to actually be present.
+        row = {header: _ls_str(r.get(snake)) for snake, header in colmap.items()}
         out.append(row)
```

**Before / after** (verified against the real `raw_gps_48_hours`, IDN 1267951):

```
before:  {IDN, GPS Type, GPS Install Date, Order}              # 3 optional headers missing
after:   {IDN, GPS Type:'IN CUSTODY', GPS Install Date:'',
          Order:'No - Warrant Used', Switched To:'',
          Switched GPS Date:'', Notes:''}                       # complete, deterministic
```

`lookup_datasets()` row counts unchanged; check-in feed still correct
(`Check in Date` and `Referral Date` both present and distinct).

### ⚠ R1 is only half-fixed in code — the other half is operational

Emitting empty headers makes discovery deterministic but **does not restore the
features** — they need real switch/relief/notes *data*. The importer
(`sharepoint_import.py`) already maps `Switched To` / `Switched GPS Date` /
`Notes`, and `_ensure_columns` will create the columns — **but only if the
GPS-48-Hours SharePoint export actually contains those headers.**

**[ACTION — verify on `ptr1`, could not be done from the Windows box]:**

```bash
# On ptr1: do the optional GPS columns exist and carry data?
/opt/ptr-knoxc/venv/bin/python3 - <<'PY'
import sqlite3; c=sqlite3.connect("/opt/ptr-knoxc/db/kh222.db")
cols=[r[1] for r in c.execute("PRAGMA table_info(raw_gps_48_hours)")]
print("has switched_to:", "switched_to" in cols,
      "| switched_gps_date:", "switched_gps_date" in cols,
      "| notes:", "notes" in cols)
for col in ("switched_to","switched_gps_date","notes"):
    if col in cols:
        n=c.execute(f"SELECT COUNT(*) FROM raw_gps_48_hours WHERE [{col}] IS NOT NULL AND [{col}]<>''").fetchone()[0]
        print(f"  non-empty {col}: {n}")
PY
```

- If the columns are **present and populated** → R1 is fully resolved on ptr1
  (the offline copy simply predates the new importer); the code hardening just
  keeps discovery stable.
- If **absent/empty** → add `Switched To`, `Switched GPS Date`, `Notes` to the
  GPS-48-Hours SharePoint list + the Power Automate "Create CSV table" column
  set, then run a `--full` import. No further code change needed.

---

## Items intentionally NOT changed (and why)

| Finding | Decision |
|---|---|
| Y1 — `overdue_check_ins`/`alerts_summary` flat 14-day rule, no L1 exclusion, stale table | Not exposed by the live app. Rather than patch dead legacy SQL, the **Go rewrite implements the real per-level windows** (Phase 4). Patching `app.py` now would be wasted effort on code being retired. |
| Y2 — normalized tables lag raw | ETL concern; the Go rewrite reads `raw_*` for the lookup math (same source as the live tool), sidestepping the lag. |
| Y3 — `_vendorOf` `.includes('IC')` false-positive risk | Real data ("IN CUSTODY") does not contain the `IC` substring, so it does not fire. Changing it would **diverge from the canonical spec**, which the live tool runs verbatim. Left as-is; noted for Go. |
| Y4 — naive `_fmt_date` | Display/legacy only; no live impact. Go uses one tz-aware parser. |

---

## Verification summary

- R2: tokenizer outputs hand-checked against grouped/space-joined/comma inputs. ✅
- R1: patched `_ls_rows` emits all 7 GPS headers against the real table. ✅
- No live business-math code changed (it is the canonical spec; must not drift).
- `pymssql` is not installed on the Windows box, so the FastAPI app itself was
  not booted here; logic was validated against `db/kh222.db` directly.

### Next step
Proceed to **Phase 4 — Go rewrite** (`PHASE_4_GO_REWRITE.md`). Acceptance test =
the `PHASE_2 §4` golden values reproduced by the Go implementation.
