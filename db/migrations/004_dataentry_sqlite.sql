-- 004_dataentry_sqlite.sql — app-owned data entry (SQLite).
--
-- Lets officers ADD defendants, payments, and check-ins from the website without
-- touching the raw_* tables (the importer full-reloads those every Sunday, so any
-- direct raw write would be wiped). These rows are merged into every read path
-- (BuildClients, LookupDatasets, EMFees) exactly like overrides/tombstones, so an
-- app-added person/payment/check-in shows up — and is computed — everywhere, and
-- survives the import. Tombstones (deleted_idns) still suppress them, and every
-- insert/delete is audited.
--
-- Column names deliberately mirror the matching raw_* columns (snake_case) so the
-- merge is a plain append — the read paths read the same keys regardless of source.
-- Idempotent (IF NOT EXISTS); the Go server also self-provisions these at startup.

CREATE TABLE IF NOT EXISTS added_defendants (
    add_id              INTEGER PRIMARY KEY AUTOINCREMENT,
    idn                 TEXT NOT NULL,
    defendant           TEXT,            -- name
    warrant_case_num    TEXT,
    pretrial_level      TEXT,
    case_status         TEXT,            -- 'Open' / 'Closed'
    supervising_officer TEXT,
    referral_date       TEXT,
    gps                 TEXT,            -- 'True' / 'False'
    gps_type            TEXT,            -- ALLIED / SCRAM / IN CUSTODY / ''
    charge_type         TEXT,
    bond_amount         TEXT,
    supervision_type    TEXT,
    order_from          TEXT,
    dma                 TEXT,
    birthdate           TEXT,
    closed_date         TEXT,
    day_adjustment      TEXT,
    author              TEXT,
    created_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_added_def_idn ON added_defendants(idn);
CREATE UNIQUE INDEX IF NOT EXISTS uq_added_def ON added_defendants(idn, IFNULL(warrant_case_num, ''));

CREATE TABLE IF NOT EXISTS added_payments (
    add_id                         INTEGER PRIMARY KEY AUTOINCREMENT,
    idn                            TEXT NOT NULL,
    case_number                    TEXT,
    payment_date                   TEXT,
    payment_amount                 TEXT,
    payment_type                   TEXT,
    officer_that_collected_payment TEXT,
    author                         TEXT,
    created_at                     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_added_pay_idn ON added_payments(idn);

CREATE TABLE IF NOT EXISTS added_check_ins (
    add_id           INTEGER PRIMARY KEY AUTOINCREMENT,
    idn              TEXT NOT NULL,
    date             TEXT,
    type_of_check_in TEXT,
    author           TEXT,
    created_at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_added_ci_idn ON added_check_ins(idn);
