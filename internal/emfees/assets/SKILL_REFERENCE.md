---
name: past-due-em-fees
description: Calculate which Knox County Pre-Trial GPS clients are behind on their electronic monitoring (EM) fees and generate a past-due memo for each one, split into Open and Closed case folders. ALWAYS use this skill when the user uploads any combination of "GPS 48 Hours", "Payments", and "New Blue Book" CSVs and asks anything about arrears, past-due, behind on payments, EM fees, GPS fees, ALLIED/SCRAM billing, or wants memos generated. Triggers include phrases like "who is behind on GPS payments", "make past due memos", "5 days behind", "generate arrears notices", "past due EM fees", "who owes us money on GPS", or simply uploading the three CSVs together.
---

# Past Due EM Fees

## What this does

Given Knox County Pre-Trial's data exports, identify every client who is 5+ days behind on their electronic monitoring fees and generate a filled-in past-due memo for each one. Outputs:

- A dated folder containing `Open/` and `Closed/` subfolders of memos (`.docx` files, one per person)
- A summary Excel workbook with Open Cases, Closed Cases, and Summary sheets

## Inputs

The user uploads three CSVs (file names usually have hashes like `Payments-49648d18.csv` but always start with these stems):

1. **GPS 48 Hours** — the source of install dates. Has `GPS Install Date`, `GPS Type` (ALLIED/SCRAM), `Case Status`, `Closed Date` columns.
2. **Payments** — every fee payment. Has `Payment Type`, `Payment Amount`, `Case Number`, `IDN`.
3. **New Blue Book** — case status and court info. Has `Case Status` (OPEN/CLOSED), `GPS Type`, `COURT`, `Closed Date`.

If any of the three is missing, ask the user to upload it before running. Don't try to proceed with two — the analysis depends on all three.

## How to run

The skill bundles a single script that does the full pipeline. Run it from the skill directory, passing the three input files. The memo template is bundled in `assets/memo_template.docx` and is picked up automatically.

```bash
python3 scripts/generate_memos.py \
    --gps-48 "<gps_48_hours>.csv" \
    --payments "<payments>.csv" \
    --blue-book "<new_blue_book>.csv" \
    --output-dir "<workspace>/Past Due EM Fee Memos (M-D-YYYY)" \
    --as-of YYYY-MM-DD \
    --verbose
```

`--as-of` defaults to today and controls how many days each Open person has been on GPS. The Closed list always uses each person's actual close date as the end of their billing period, so `--as-of` doesn't affect Closed totals.

`--output-dir` is the dated folder that will be created. Inside it the script makes `Open/`, `Closed/`, and `Past_Due_Summary.xlsx`.

## Methodology (so you can answer questions about the numbers)

**Rates.** ALLIED = $8/day, SCRAM = $15/day. Anyone with a different GPS Type (IN CUSTODY, blank, etc.) is skipped — they're not being billed for daily monitoring.

**Threshold.** 5+ days behind. Computed as `(owed − paid) / daily_rate >= 5`.

**Payment matching.** Sum payments tagged `GPS`/`Allied`/`Scram` (case-insensitive). Exclude `GPS Install Fee`, `PTR`, `Drug Screen` — those are one-time or unrelated. Match payments to the person by `(Case Number, IDN)` first, fall back to IDN-only if no payments on that case.

**Open cases.** Person appears in the GPS 48 Hours file with `Case Status = OPEN` and an `Install Date`. Days enrolled = `as_of − install_date`.

**Closed cases.** Person had GPS (`GPS = True` in Blue Book or `GPS Type` set) and all their cases are now closed. Days = `closed_date − start_date`. The Closed Date comes from the Blue Book (latest across all the person's records). The start date is harder — closed cases often don't have install dates, so the script falls back in this order:

1. Install Date from GPS 48 Hours, if the person is also in that file
2. First daily-fee payment date from Payments
3. Released to Hilltop Date from Blue Book
4. Referral Date from Blue Book

The "Start Source" column on the Closed Cases sheet records which one was used so the user can audit each row.

**Junk filter.** Skip rows where the Defendant name starts with `!!!` or contains "test" — these are scratch entries the user has put in the system.

## What to do with the output

Save the dated folder to the user's workspace (usually `hill top reports/`) and share computer:// links to both the folder and the summary `.xlsx`. Report the counts and totals from the Summary sheet:

> Found N Open ($X behind) and M Closed ($Y behind). Memos are in `Open/` and `Closed/`.

The Summary sheet highlights:
- **Pink rows** on Closed = monitoring span > 2 years (likely overstated, review before sending)
- **Yellow rows** on Closed = Referral Date was used as start (no payment history; may overstate days)
- **Pink rows** on Open = 30+ days behind
- **Yellow rows** on Open = 14+ days behind

Mention these caveats to the user so they know which memos to spot-check.

## Memo template details

The template in `assets/memo_template.docx` uses U+2002 (em-space) runs as placeholders. Each fillable field is 5 consecutive em-space runs in a row. The script finds those clusters and replaces them in this order:

| Paragraph | Cluster | Field |
| --- | --- | --- |
| P10 (Date/Court line) | 1 | Date (from `--as-of` formatted as M/D/YYYY) |
| P10 | 2 | Court (from Blue Book COURT column, blank if not set) |
| P13 (Defendant line) | 1 | Defendant name |
| P13 | 2 | IDN |
| P13 | 3 | Warrant/Docket (Case Number) |
| P17 (Body) | 1 | GPS Type (ALLIED or SCRAM) |
| P17 | 2 | Arrearage (dollar string like "$1,234.00") |

If a value is empty/None the cluster is left as em-spaces so the officer can fill it in by hand. This commonly happens for `Court` on Closed cases (the Blue Book doesn't populate COURT for closed cases) and for `Case Number` on a handful of closed cases where the Blue Book has placeholder values like "needs#001".

## If something looks off

- **Way too few Open results**: check that the GPS 48 Hours file is the recent multi-row version (700+ rows with both OPEN and CLOSED). Older exports had only ~300 rows of active installs.
- **A person you expect is missing**: check their GPS Type — if it's blank or IN CUSTODY in both 48 Hours and Blue Book, the script can't determine their daily rate and skips them.
- **HUMPHREY-style 6-year-old start dates on closed cases**: the script is using a very old first payment as the start. These are legit data artifacts, not script bugs. Flag them to the user for manual review.
- **All memo fields blank**: the em-space character (U+2002) in the script got corrupted to a regular space. Verify `EM` in the script equals `' '`, not `' '`.
