# KH222 Knox County Sheriff Pre-Trial Database

Turn-key SQL database built from five SharePoint / JIMS exports.

## What's in this folder

```
kh222_db/
├─ kh222.db                 SQLite, fully populated, indexed, FK-validated
├─ schema_azure_sql.sql     Idempotent T-SQL DDL (Azure SQL / SQL Server 2016+)
├─ load_azure.sql           TRUNCATE + BULK INSERT template (+ Blob setup block)
├─ csv_clean/               One UTF-8 CSV per table, ready for BULK INSERT
│   ├─ raw_master_list.csv
│   ├─ raw_blue_book.csv
│   ├─ raw_payments.csv
│   ├─ raw_check_ins.csv
│   ├─ raw_gps_48_hours.csv
│   ├─ defendants.csv
│   ├─ cases.csv
│   ├─ payments.csv
│   ├─ check_ins.csv
│   └─ gps_events.csv
├─ build_db.py              Source ETL - rebuilds kh222.db from the 5 uploads
├─ export_azure.py          Source - rebuilds csv_clean/ + *.sql from kh222.db
└─ README.md                (this file)
```

## Schema

Two tiers — raw tables preserve source fidelity; normalized tables are joined on `idn`.

### Raw (column-for-column from source)

| Table               | Rows   | Source file                        |
|---------------------|--------|------------------------------------|
| `raw_master_list`   | 32,279 | `KH222 Master List.xlsx` (`ALL`)   |
| `raw_blue_book`     | 3,509  | `New Blue Book.csv`                |
| `raw_payments`      | 1,713  | `Payments (2).csv`                 |
| `raw_check_ins`     | 2,046  | `Check Ins (1).csv`                |
| `raw_gps_48_hours`  | 698    | `GPS 48 Hours (1).csv`             |

The three large event CSVs get a synthetic IDENTITY PK (`payment_id`, `check_in_id`, `gps_id`) since the source has no stable key.

### Normalized

| Table         | Rows   | PK         | FK       | Notes                                           |
|---------------|--------|------------|----------|-------------------------------------------------|
| `defendants`  | 25,893 | `idn`      | —        | Blue Book + Master List, deduped by IDN.        |
| `cases`       | 9,864  | `case_id`  | `idn`    | Split multi-case strings from blue_book + payments. |
| `payments`    | 1,713  | `payment_id` | `idn`  | Cleaned mirror of `raw_payments`.               |
| `check_ins`   | 2,046  | `check_in_id` | `idn` | Cleaned mirror of `raw_check_ins`.              |
| `gps_events`  | 698    | `gps_id`   | `idn`    | Cleaned mirror of `raw_gps_48_hours`.           |

`defendants.source` distribution: **both: 2,621 · blue_book only: 688 · master_list only: 22,584**.

### Indexes

`idn` on every event table, `case_number` on `cases`, event-date columns on `payments` and `check_ins`.

### View

```sql
v_defendant_summary  -- one row per defendant + counts of cases / check-ins / payments / GPS + SUM(payment_amount)
```

## Why this shape?

The Master List is 32K rows of partial/closed-case info with only 8 columns; the Blue Book is the 3.5K-row "active roster" with full 40-column detail. Joining them on `idn` gives a single defendant table that's complete without losing either source — the `source` column makes it easy to filter (`WHERE source = 'blue_book'` = currently tracked; `WHERE source = 'both'` = historical defendants still in active supervision).

Keeping `raw_*` tables alongside the normalized tables means downstream analysts can always get back to the original SharePoint export without re-running the pipeline.

## Verification (what passed)

* `PRAGMA foreign_key_check` → **0 violations**
* Orphan events (`idn` not in `defendants`): **0** in `cases` / `payments` / `check_ins` / `gps_events`
* All 10 table row counts match spec exactly
* `v_defendant_summary` returns clean rows with counts and summed payments

## Loading into Azure — three ways

### 1. Quick SQLite copy for local work

```bash
sqlite3 kh222.db .dump | sqlite3 /path/to/local.db
# or just open kh222.db in DBeaver / DB Browser for SQLite
```

### 2. Re-create in Azure SQL via DDL + BULK INSERT

```sql
-- Run once, in order:
:r schema_azure_sql.sql     -- drops + creates tables, FKs, indexes, view
:r load_azure.sql           -- truncates + BULK INSERTs each CSV
```

The loader expects the CSVs in a `csv_clean/` folder co-located with the SQL script, or on Azure Blob if you uncomment the `CREATE EXTERNAL DATA SOURCE` block at the top of `load_azure.sql` and fill in an SAS token.

### 3. Azure Data Factory pipeline

1. Copy `csv_clean/*.csv` to a Blob container.
2. Run `schema_azure_sql.sql` via **Script** activity against the target DB.
3. Add one **Copy Data** activity per CSV → table. Set source = the Blob CSV (first row header, UTF-8); sink = the matching `dbo.<table>`; mapping = explicit.
4. Chain a final **Script** activity to run the row-count check at the bottom of `load_azure.sql`.

## Parsing decisions applied

| Area             | Rule                                                                       |
|------------------|----------------------------------------------------------------------------|
| CSV header       | SharePoint exports row 0 = ListSchema JSON blob → stripped, row 1 = real header |
| Encoding         | `utf-8-sig` for CSV reads                                                  |
| Column names     | snake_case; `%23` → `num`; collisions suffixed `_2`, `_3`, …               |
| Dates            | Try ISO first, fall back `%m/%d/%Y %H:%M`, `%m/%d/%Y`, `%Y-%m-%d %H:%M:%S`, `%Y-%m-%d`; unparseable kept raw |
| Currency         | `$20.00` → `20.0` (float)                                                  |
| Booleans         | `True`/`Yes`/`1` → 1; `False`/`No`/`0` → 0                                 |
| Multi-case str   | `@1632112` or `@1606962, @1641152` → kept raw in `raw_*`; split into rows in `cases` with `@` stripped |

## Sample queries

```sql
-- Top 5 payers
SELECT idn, defendant_name, total_paid_calc
FROM v_defendant_summary
ORDER BY total_paid_calc DESC LIMIT 5;

-- Active GPS with victim
SELECT g.idn, g.defendant, g.victim, g.gps_install_date, g.case_status
FROM gps_events g WHERE g.case_status = 'OPEN' AND g.gps_install_date IS NOT NULL;

-- Check-ins per supervising officer, last 90 days
SELECT supervising_officer, COUNT(*) AS ci_count
FROM check_ins WHERE check_in_date >= date('now','-90 day')
GROUP BY supervising_officer ORDER BY ci_count DESC;

-- Defendants in master list but not Blue Book (historical / closed-only)
SELECT idn, defendant_last_name, pretrial_level, charge_type, referral_date
FROM defendants WHERE source = 'master_list';
```

## Known quirks

* **Mixed date formats** — the source mixes ISO `2026-02-23T19:42:00Z`, US `11/25/2025 12:50`, and Excel datetime objects. We normalize to ISO-8601 where we can and leave anything else as the original string. On Azure, all date columns are `NVARCHAR(50)` for that reason — cast with `TRY_CONVERT(datetime2, …)` downstream.
* **`order` is a T-SQL reserved word** — `raw_gps_48_hours.order` (the court-order column) is bracketed as `[order]` in `schema_azure_sql.sql`. In the normalized `gps_events` table it's renamed to `court_order`.
* **Case numbers** — raw tables keep the `@`-prefixed / comma-joined strings as-is. Use the normalized `cases` table when you need one row per case.
* **OneDrive + SQLite** — the outputs folder is OneDrive-mounted and can't lock `.db` files. `build_db.py` builds in `/tmp` and byte-copies the finished file in. If you re-run, the script handles the overwrite; if you edit manually, copy via `cp`, not a program that opens the file in write mode.
* **Defendant names** — Blue Book stores `"LAST, FIRST M"` in `defendant`; Master List stores just the last name in `defendant_last_name`. The `defendants` table carries both: `defendant_name` (full, from Blue Book) and `defendant_last_name` (from Master List, backfilled from the Blue Book full name where needed).

## Re-running the pipeline

```bash
# 1. Rebuild the SQLite file from the 5 source files
python build_db.py --uploads /path/to/uploads --out .

# 2. Regenerate CSVs + Azure SQL scripts from the SQLite file
python export_azure.py --db kh222.db --out .
```

Both scripts stage writes in `/tmp` and copy into the output folder at the end to avoid OneDrive file-locking errors on the final `.db`.
