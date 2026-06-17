-- 008_referral_fields_sqlite.sql — widen added_defendants to the full referral.
--
-- The console "New Client Referral" wizard now mirrors the SharePoint exports
-- (New Blue Book + GPS 48 Hours), so the app captures what SharePoint captures
-- instead of dropping the extra fields into a note. These columns reuse the
-- canonical raw_blue_book / raw_gps_48_hours snake_case names, so they merge into
-- the blue-book row set by column name in every read path (BuildClients,
-- LookupDatasets, EMFees) with no read-side wiring — exactly like the existing
-- added_defendants columns.
--
-- gps_install_date / switched_to / switched_gps_date are read by the EM-fee
-- engine's blue-book pass (internal/emfees), so an app-entered GPS client bills
-- correctly; the rest are faithfully stored reference data.
--
-- NOTE: the GPS file's column is literally `order` (a SQL reserved word). We store
-- court-ordered GPS as `court_order` to avoid quoting; the blue-book dataset has no
-- `order` consumer, so this is purely captured reference data.
--
-- Idempotent at the app layer: the Go server runs addColumnIfMissing() for each of
-- these at startup (internal/db/admin.go), so this file is the historical record.
-- (SQLite has no ADD COLUMN IF NOT EXISTS; re-running this file on a DB that already
-- has the columns will error — rely on the server's startup migration instead.)

ALTER TABLE added_defendants ADD COLUMN bond_conditions          TEXT;
ALTER TABLE added_defendants ADD COLUMN court                    TEXT;
ALTER TABLE added_defendants ADD COLUMN victim                   TEXT;
ALTER TABLE added_defendants ADD COLUMN victim_idn               TEXT;
ALTER TABLE added_defendants ADD COLUMN victim_2                 TEXT;
ALTER TABLE added_defendants ADD COLUMN victim_2_idn             TEXT;
ALTER TABLE added_defendants ADD COLUMN victim_3                 TEXT;
ALTER TABLE added_defendants ADD COLUMN victim_3_idn             TEXT;
ALTER TABLE added_defendants ADD COLUMN victim_time_48           TEXT;
ALTER TABLE added_defendants ADD COLUMN victim_accept_deny_gps   TEXT;
ALTER TABLE added_defendants ADD COLUMN gps_install_date         TEXT;
ALTER TABLE added_defendants ADD COLUMN da_emailed               TEXT;
ALTER TABLE added_defendants ADD COLUMN switched_to              TEXT;
ALTER TABLE added_defendants ADD COLUMN switched_gps_date        TEXT;
ALTER TABLE added_defendants ADD COLUMN paid                     TEXT;
ALTER TABLE added_defendants ADD COLUMN court_order              TEXT;
ALTER TABLE added_defendants ADD COLUMN comments                 TEXT;
ALTER TABLE added_defendants ADD COLUMN received_signed_copy_date TEXT;
ALTER TABLE added_defendants ADD COLUMN contact_date             TEXT;
ALTER TABLE added_defendants ADD COLUMN released_to_hilltop_date TEXT;
ALTER TABLE added_defendants ADD COLUMN ptr_successfully_completed TEXT;
