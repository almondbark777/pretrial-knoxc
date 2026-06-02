# Case Console (`/console`) — demo runbook

> A click-path for demoing **Direction B** (the Case Console) and a clear map of
> **what's real vs. demo-safe**, so nothing surprises you mid-demo. Companion to
> `CONSOLE_DASHBOARD.md` (what was built + why). Branch `feature/console-dashboard`.

The point of this build is to put **two UX directions side by side** so a direction
can be chosen:

- **Direction A — `/dashboard`** (existing): dark top-nav app, admin & data-entry feel.
- **Direction B — `/console`** (this): light enterprise-SaaS shell — navy left sidebar,
  white panels, dense tables, status chips, dark-mode toggle.

Both read from the **same server-side math** (`computeStats` / `behindRoster` /
`missedCheckInsRoster` / `ComputeCheckIns` / `ComputePTRFees` / `ComputeGPS`), so the
numbers match each other and the tracker exactly — the console is a pure presentation layer.

---

## Before you start

- Run the server (preview uses port 8099, password `test`; prod uses the real `APP_PASSWORD`).
- Land on the **client tracker** at `/`. It now has two top-bar buttons:
  **"Admin & Data-Entry →"** (`/dashboard`, Direction A) and **"Case Console →"** (`/console`, Direction B).
- Click **Case Console →**. (Every console page also has a **"← Client Tracker"** link back.)

---

## Suggested 5-minute click-path

1. **Dashboard (`/console`)** — lead with the division caseload.
   - 5 KPI cards, each a **deep-link**: **Active Clients** → roster (active), **Check-ins Due
     Today** → roster, **Court Dates This Week** → calendar, **Open Violations** → compliance,
     **Overdue Check-ins (mo.)** → compliance. Click **Overdue Check-ins** to jump to Compliance.
   - **Alerts Needing Attention** + **Today's Schedule** panels below (each row links to the
     client record). Toggle **All ⇄ My caseload** (top-right) to scope to the officer's own
     caseload vs. the division, with a graceful empty-state when nothing's on your caseload.

2. **Clients (`/console/clients`)** — the workhorse roster.
   - Type in the filter — live filter as you type; combine with Status / Level / Officer /
     Compliance / GPS dropdowns. Filters are in the **URL** (shareable / bookmarkable).
   - Click a column header to **sort**; the roster paginates at 50/page.
   - **Bulk select:** tick a few rows (or the header box for the page, then "Select all N
     filtered") → the action bar appears → **Export selected** downloads a CSV of exactly
     those clients. (A double-click never duplicates anything — see "demo-safe" below.)
   - Hover a row → **✓ Check-in** quick-action (this one is **real** — it persists).
   - Type a nonsense filter → the **empty-state** ("No clients match… Clear all filters").
   - **Export CSV** (top-right) downloads the current filtered roster.

3. **A client record (`click any row`)** — the depth of the product.
   - Sticky header + badges + quick actions; **7 tabs** (Case Summary, Conditions, Check-ins,
     Court, Payments/Fees, Documents, Activity).
   - Open **Add Note** or **Log Check-in** → fill it → save. It **persists** and appears in the
     **Activity** timeline (reverse-chron, merging events + notes + violations + court + reminders).
   - **Court** tab → **Add Court Date**, then **Log outcome** (e.g. "Appeared — plea entered" +
     next date) — the FTA-tracking step; shows as a ✓ chip. **Real.**
   - The **⋯** overflow menu shows the rest (Add tag / Pin / Correct field / Waive fee / Print).

4. **Compliance (`/console/compliance`)** — "matches the tracker."
   - Behind-on-GPS roster with **Owed / Paid / Behind** columns + waiver flag, and the
     Missed-Check-Ins roster. Call out that **Behind on GPS = 133**, identical to the tracker's
     "Behind on Coverage" and `/api/stats` — same math, different skin. CSV export + print.

5. **Reports (`/console/reports`)** — the "so what."
   - Population by level, GPS vendor mix, caseload by officer, fees outstanding, plus rate
     cards: check-in compliance, GPS coverage compliance, fee collection. Strong talking point:
     **PTR fee collection ≈ 0.7%** — the tool surfaces it instantly.

6. **As-of time travel** — set the **AS OF** date in the top bar to a past date and watch the
   whole console re-compute (e.g. Due-today and Missed counts change) — same `trackDate`
   contract as the tracker, no separate logic.

7. **Polish to drop in** — press **`?`** for the keyboard-shortcut overlay (`/` search,
   `g`+letter nav, `n` new note, `Esc` close); toggle **dark mode** (◐ in the top bar);
   resize the window to show the sidebar collapse to an icon rail (responsive); **Print** a
   record or report for a clean PDF.

---

## What's REAL vs. DEMO-SAFE (read this before presenting)

**Real (reads live data / persists):**
- Every read view — dashboard, roster, record tabs, calendar, compliance, reports, admin
  audit + tombstones — all from real data, numbers correct and matching the tracker.
- Writes via the CSRF-guarded `/admin/*` endpoints (extension tables + `audit_log` only,
  **never `raw_*`**): **Add Note, Log Check-in, Add Court Date, Log court outcome, Record
  violation**, and the roster's **✓ Check-in** quick-action.
- **New Intake** wizard's final step creates a **real native client** (lands on the new record).
- **CSV exports** (filtered roster, selected rows, compliance) and **as-of** time travel.

**Demo-safe (opens a tidy modal / toast, no write this pass — by design):**
- **Send Reminder** — *log-only*: records the reminder marked "logged (not sent)"; no SMS/email
  provider is wired yet.
- **Bulk Check-in selected**, **Add tag**, **Pin client**, **Correct field (override)**,
  **Waive fee**, **Upload Document** — confirm with a modal/toast; write path is a later milestone.
- **Admin** roles / conditions library / reminder templates — placeholders (the audit trail and
  tombstones on that page are real).

There are **no dead links or error pages** — every nav item and button does something graceful
(audited). A double-click on a Save button can't create a duplicate (the submit button disables
and shows "Saving…").

---

## Direction A vs. Direction B — talking points

| | **A — `/dashboard`** | **B — `/console`** |
|---|---|---|
| Feel | Dark top-nav, admin/data-entry | Light enterprise SaaS, left sidebar |
| Navigation | Top tabs | Persistent sidebar + global search + `g`-nav |
| Roster | Table | Table + live filter, bulk select, quick actions, empty-state |
| Record | Profile page | Sticky header + 7 tabs + activity timeline |
| Status | Color | **Wong colorblind-safe** color **+ icon + text** |
| Extras | — | As-of time travel, dark mode, `?` shortcuts, print, mobile rail |
| Data | Same source-of-truth math | Same source-of-truth math |

Both are real and clickable today. The decision is **UX direction**, not capability — the
backend and the numbers are shared.

---

## If something looks off

- **Numbers:** they come from the shared compute layer; if a console number looks wrong, it
  will look the same on `/dashboard` and the tracker (it's not a console bug). Check `/api/stats`.
- **A write didn't show:** confirm you're on the same client record; writes redirect back with a
  toast and appear in **Activity**. (Reminders are intentionally log-only.)
- **Reset the demo DB** (preview only): stop the server, delete `db/_smoke_test.db`
  (+ `-shm`/`-wal`), restart — it re-copies a pristine scratch DB.
