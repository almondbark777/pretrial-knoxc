# Phase 10 — Data entry (add defendants, payments, check-ins)

> Append-only paper trail. Done 2026-05-31 (Opus 4.8, 1M).

## What this is

The first step of the stated long-term direction — *data entry moving into the app*
(the app eventually replacing SharePoint as system of record). Officers can now,
from the website:

- **Add a client** (new defendant) — `/admin/add_defendant` (linked "+ Add client"
  on the dashboard).
- **Add a payment** to an existing client — form on the profile.
- **Add a check-in** to an existing client — form on the profile.

## The hard constraint and how it's handled

`raw_*` is owned by the daily SharePoint importer, which **full-reloads** those
tables every Sunday. The app must never write there (Brief 5.4 / CLAUDE.md), or the
write would be wiped. So app-entered records go to **new app-owned tables** and are
**merged into every read path** — the same importer-proof pattern as overrides and
tombstones:

- New tables (migration `004_dataentry_sqlite.sql`, also self-provisioned in
  `EnsureSchema`): `added_defendants`, `added_payments`, `added_check_ins`. Their
  columns deliberately mirror the matching `raw_*` columns (snake_case), so the
  merge is a plain append.
- **Merged in three places** via `queryMapsIfExists` (tolerant of a pre-004 DB):
  - `BuildClients` — added rows join the bb/ci/pm sets → app-added people, payments,
    and check-ins flow into the profile, rosters, stats, calendars, and the
    compute layer (check-in compliance, PTR/GPS fee math) like any other record.
  - `LookupDatasets` — so the bundled client tracker shows them too.
  - `EMFees` — so an app-entered GPS payment reduces the arrears, and an app-added
    person is considered, on the Past-Due EM Fees report.
- **Tombstones still apply**: a supervisor can delete an app-added person/case and
  it vanishes everywhere (verified by test). That delete is the backstop for a
  mistaken entry.
- **Every write is audited** (`defendant_add` / `payment_add` / `checkin_add` and
  the `*_delete` actions) and shows in the supervisor audit viewer.

## Files

- `db/migrations/004_dataentry_sqlite.sql` + the same DDL inlined in
  `internal/db/admin.go` `ensureSchemaSQL`.
- `internal/db/dataentry.go` — `NewDefendant`, `IDNExistsInRoster`, `AddDefendant`
  (rejects blank IDN/Name and any IDN already in the roster), `AddPayment`,
  `AddCheckIn`, `List/DeleteAddedPayments`, `List/DeleteAddedCheckIns`. All inserts
  go through the existing `txAddWithAudit`; deletes through `txDeleteByID`.
- `internal/db/{db,lookup_data,emfees}.go` — the three merges + `queryMapsIfExists`.
- `internal/db/extension.go` — `LoadExtras` now also loads the app-entered
  payments/check-ins so the profile can list and delete them.
- `internal/models/models.go` — `AddedPayment`, `AddedCheckIn`, extended
  `DefendantExtras`.
- `internal/handlers/dataentry.go` — `AddDefendantForm/AddDefendant`,
  `AddPayment/DeleteAddedPayment`, `AddCheckIn/DeleteAddedCheckIn`.
- `templates/add_defendant.html` (new) + `templates/profile.html` (record-payment /
  record-check-in panels with app-entered lists) + dashboard "+ Add client".
- Routes under `/admin/*` (CSRF-guarded). `cmd/server/main.go`.

## Gating

Adding is open to **any allowed officer** (audited) — consistent with the other
data-entry CRUD (notes/tags/court dates/…), and matching the "data entry moves into
the app" goal. Deletes/overrides remain supervisor-only, and supervisor delete is
the backstop for a wrong add. (To restrict adding to supervisors, add a
`requireSupervisor` gate at the top of the handlers in `internal/handlers/dataentry.go`
— a one-line change each.)

### Payment types that register
GPS coverage credits exact `GPS` / `Allied` / `SCRAM`; PTR credits any type
containing "PTR" (e.g. `PTR Fee`); the EM-fee engine uses the broader daily set.
The form's payment-type options use the real strings so entries register in the
right calculation.

## Verification
- `internal/db/dataentry_test.go`: add → appears in `BuildClients` with correct
  fields; duplicate IDN rejected; added payment/check-in flow into
  `client.Payments`/`CheckIns`; list + delete; **tombstone suppresses an app-added
  person**; audit rows written for all three actions.
- Live end-to-end (login → CSRF → POST): added client appears on the **profile**,
  the **search API**, and the **bundled tracker feed**; an added SCRAM $120 payment
  shows in the computed **GPS "paid $120"** total and as an app-entry row; check-in
  records. `go build`/`vet`/`gofmt`/`test` all green.

## Hard rules honored
Native SQLite only · **no writes to `raw_*`** (app-owned tables, merged on read) ·
importer-proof (survives the Sunday reload) · tombstones suppress app-added rows ·
every write audited · CSRF on all POSTs · same dark Wong-palette theme.

## Follow-ups / notes
- New defendants need a not-yet-existing IDN (the form rejects an IDN already in the
  roster); adding a *case* to an existing person or full *edit* of imported fields
  is out of scope here (supervisors use field overrides for corrections).
- IDN uniqueness is checked against `raw_blue_book` + `added_defendants` at add time;
  during the transition, the same IDN later arriving from SharePoint would create a
  second (raw) case row for that person — handled gracefully by the multi-case logic.
