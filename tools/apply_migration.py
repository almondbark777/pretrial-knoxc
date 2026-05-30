"""Apply a migration .sql file (split on 'GO') to Azure SQL.

Usage:
    .venv/Scripts/python.exe tools/apply_migration.py db/migrations/001_app_extensions.sql
"""
import os, sys, pathlib
from dotenv import load_dotenv

# Load from webapp/.env
load_dotenv(pathlib.Path(__file__).parent.parent / "webapp" / ".env")
import pymssql

if len(sys.argv) != 2:
    print("Usage: apply_migration.py <path-to-sql>")
    sys.exit(1)

sql_path = pathlib.Path(sys.argv[1])
sql_text = sql_path.read_text(encoding="utf-8")

# Split on GO (statement separator)
statements = [s.strip() for s in sql_text.split("\nGO") if s.strip()]

import time
def _connect_with_retry():
    last = None
    for attempt in range(6):
        try:
            return pymssql.connect(
                server=os.environ["DB_SERVER"],
                user=os.environ["DB_USER"],
                password=os.environ["DB_PASSWORD"],
                database=os.environ["DB_NAME"],
                tds_version="7.4",
                charset="UTF-8",
                login_timeout=60,
                autocommit=True,
            )
        except pymssql.OperationalError as e:
            last = e
            if "40613" in str(e) or "not currently available" in str(e):
                wait = 8 * (attempt + 1)
                print(f"  DB auto-paused — waiting {wait}s for resume (attempt {attempt+1}/6)…")
                time.sleep(wait)
                continue
            raise
    raise last

print("Connecting (this may take 15-30s if the DB is paused)…")
conn = _connect_with_retry()
print("Connected.")
cur = conn.cursor()
for i, stmt in enumerate(statements, 1):
    print(f"[{i}/{len(statements)}] applying batch ({len(stmt)} chars)…")
    try:
        cur.execute(stmt)
        # drain any result rows for safety
        try:
            while cur.fetchall():
                pass
        except Exception:
            pass
        print("    ok")
    except Exception as e:
        print(f"    FAILED: {e}")
        raise

print("\nMigration applied.")
conn.close()
