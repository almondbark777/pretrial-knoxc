-- =====================================================
-- KH222 Pre-Trial  -  BULK INSERT loader template
-- =====================================================

/* ---- Azure Blob setup (uncomment + fill in, run once) ----
CREATE MASTER KEY ENCRYPTION BY PASSWORD = '<strong-password>';
GO

CREATE DATABASE SCOPED CREDENTIAL kh222_blob_cred
    WITH IDENTITY = 'SHARED ACCESS SIGNATURE',
    SECRET = '<sas-token-without-leading-?>';
GO

CREATE EXTERNAL DATA SOURCE kh222_blob
    WITH (TYPE = BLOB_STORAGE,
          LOCATION = 'https://<account>.blob.core.windows.net/<container>',
          CREDENTIAL = kh222_blob_cred);
GO
---- end Azure Blob setup ---- */

-- Truncate child-first.
TRUNCATE TABLE dbo.cases;
TRUNCATE TABLE dbo.payments;
TRUNCATE TABLE dbo.check_ins;
TRUNCATE TABLE dbo.gps_events;
TRUNCATE TABLE dbo.defendants;
TRUNCATE TABLE dbo.raw_blue_book;
TRUNCATE TABLE dbo.raw_master_list;
TRUNCATE TABLE dbo.raw_payments;
TRUNCATE TABLE dbo.raw_check_ins;
TRUNCATE TABLE dbo.raw_gps_48_hours;
GO

-- ---- Load raw_gps_48_hours ----
BULK INSERT dbo.raw_gps_48_hours
FROM 'csv_clean/raw_gps_48_hours.csv'  -- or DATA_SOURCE = 'kh222_blob', 'csv_clean/raw_gps_48_hours.csv'
WITH (
    FORMAT = 'CSV',
    FIRSTROW = 2,
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '0x0a',
    CODEPAGE = '65001',
    FIELDQUOTE = '"',
    TABLOCK
);
GO

-- ---- Load raw_check_ins ----
BULK INSERT dbo.raw_check_ins
FROM 'csv_clean/raw_check_ins.csv'  -- or DATA_SOURCE = 'kh222_blob', 'csv_clean/raw_check_ins.csv'
WITH (
    FORMAT = 'CSV',
    FIRSTROW = 2,
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '0x0a',
    CODEPAGE = '65001',
    FIELDQUOTE = '"',
    TABLOCK
);
GO

-- ---- Load raw_payments ----
BULK INSERT dbo.raw_payments
FROM 'csv_clean/raw_payments.csv'  -- or DATA_SOURCE = 'kh222_blob', 'csv_clean/raw_payments.csv'
WITH (
    FORMAT = 'CSV',
    FIRSTROW = 2,
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '0x0a',
    CODEPAGE = '65001',
    FIELDQUOTE = '"',
    TABLOCK
);
GO

-- ---- Load raw_master_list ----
BULK INSERT dbo.raw_master_list
FROM 'csv_clean/raw_master_list.csv'  -- or DATA_SOURCE = 'kh222_blob', 'csv_clean/raw_master_list.csv'
WITH (
    FORMAT = 'CSV',
    FIRSTROW = 2,
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '0x0a',
    CODEPAGE = '65001',
    FIELDQUOTE = '"',
    TABLOCK
);
GO

-- ---- Load raw_blue_book ----
BULK INSERT dbo.raw_blue_book
FROM 'csv_clean/raw_blue_book.csv'  -- or DATA_SOURCE = 'kh222_blob', 'csv_clean/raw_blue_book.csv'
WITH (
    FORMAT = 'CSV',
    FIRSTROW = 2,
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '0x0a',
    CODEPAGE = '65001',
    FIELDQUOTE = '"',
    TABLOCK
);
GO

-- ---- Load defendants ----
BULK INSERT dbo.defendants
FROM 'csv_clean/defendants.csv'  -- or DATA_SOURCE = 'kh222_blob', 'csv_clean/defendants.csv'
WITH (
    FORMAT = 'CSV',
    FIRSTROW = 2,
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '0x0a',
    CODEPAGE = '65001',
    FIELDQUOTE = '"',
    TABLOCK
);
GO

-- ---- Load gps_events ----
BULK INSERT dbo.gps_events
FROM 'csv_clean/gps_events.csv'  -- or DATA_SOURCE = 'kh222_blob', 'csv_clean/gps_events.csv'
WITH (
    FORMAT = 'CSV',
    FIRSTROW = 2,
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '0x0a',
    CODEPAGE = '65001',
    FIELDQUOTE = '"',
    TABLOCK
);
GO

-- ---- Load check_ins ----
BULK INSERT dbo.check_ins
FROM 'csv_clean/check_ins.csv'  -- or DATA_SOURCE = 'kh222_blob', 'csv_clean/check_ins.csv'
WITH (
    FORMAT = 'CSV',
    FIRSTROW = 2,
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '0x0a',
    CODEPAGE = '65001',
    FIELDQUOTE = '"',
    TABLOCK
);
GO

-- ---- Load payments ----
BULK INSERT dbo.payments
FROM 'csv_clean/payments.csv'  -- or DATA_SOURCE = 'kh222_blob', 'csv_clean/payments.csv'
WITH (
    FORMAT = 'CSV',
    FIRSTROW = 2,
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '0x0a',
    CODEPAGE = '65001',
    FIELDQUOTE = '"',
    TABLOCK
);
GO

-- ---- Load cases ----
BULK INSERT dbo.cases
FROM 'csv_clean/cases.csv'  -- or DATA_SOURCE = 'kh222_blob', 'csv_clean/cases.csv'
WITH (
    FORMAT = 'CSV',
    FIRSTROW = 2,
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '0x0a',
    CODEPAGE = '65001',
    FIELDQUOTE = '"',
    TABLOCK
);
GO

-- ---- Post-load row-count sanity ----
SELECT 'raw_master_list' AS tbl, COUNT(*) FROM dbo.raw_master_list  UNION ALL
SELECT 'raw_blue_book',         COUNT(*) FROM dbo.raw_blue_book    UNION ALL
SELECT 'raw_payments',          COUNT(*) FROM dbo.raw_payments     UNION ALL
SELECT 'raw_check_ins',         COUNT(*) FROM dbo.raw_check_ins    UNION ALL
SELECT 'raw_gps_48_hours',      COUNT(*) FROM dbo.raw_gps_48_hours UNION ALL
SELECT 'defendants',            COUNT(*) FROM dbo.defendants       UNION ALL
SELECT 'cases',                 COUNT(*) FROM dbo.cases            UNION ALL
SELECT 'payments',              COUNT(*) FROM dbo.payments         UNION ALL
SELECT 'check_ins',             COUNT(*) FROM dbo.check_ins        UNION ALL
SELECT 'gps_events',            COUNT(*) FROM dbo.gps_events;
GO