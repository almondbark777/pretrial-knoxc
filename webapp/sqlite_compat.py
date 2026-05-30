"""SQLite shim that mimics the slice of pymssql's API used by queries.py.

Translates T-SQL to SQLite via sqlglot at execute time and post-processes
patterns sqlglot doesn't natively handle (TRY_CONVERT semantics, %s
placeholders, dbo. schema prefix, custom UDFs). Used when DB_BACKEND=sqlite
is set in the env.

Why: lets us point the local app at db/kh222.db (the offline SQLite copy
of the Azure DB) without hitting the Azure SQL firewall.
"""
from __future__ import annotations

import re
import sqlite3
from datetime import datetime

import sqlglot


_DATE_FORMATS = (
    "%Y-%m-%dT%H:%M:%S",
    "%Y-%m-%d %H:%M:%S",
    "%Y-%m-%d",
    "%m/%d/%Y %H:%M",
    "%m/%d/%Y",
)


def _try_parse_date(s):
    """Mimic SQL Server's TRY_CONVERT(datetime2, x): ISO string or NULL."""
    if s is None:
        return None
    s = str(s).strip()
    if not s:
        return None
    try:
        return datetime.fromisoformat(s.replace("Z", "+00:00")).strftime("%Y-%m-%d %H:%M:%S")
    except Exception:
        pass
    for fmt in _DATE_FORMATS:
        try:
            return datetime.strptime(s, fmt).strftime("%Y-%m-%d %H:%M:%S")
        except ValueError:
            continue
    return None


def _register_udfs(conn: sqlite3.Connection) -> None:
    conn.create_function(
        "YEAR", 1,
        lambda d: int(str(d)[:4]) if d and str(d)[:4].isdigit() else None,
    )
    conn.create_function(
        "MONTH", 1,
        lambda d: int(str(d)[5:7]) if d and len(str(d)) >= 7 and str(d)[5:7].isdigit() else None,
    )
    conn.create_function(
        "DATE_FROM_PARTS", 3,
        lambda y, m, d: f"{int(y):04d}-{int(m):02d}-{int(d):02d}",
    )
    conn.create_function("TRY_PARSE_DATE", 1, _try_parse_date)


_DBO_RE = re.compile(r"\bdbo\.", re.IGNORECASE)
_PCT_S_RE = re.compile(r"%s")
_CAST_TS_RE = re.compile(
    r"CAST\(\s*([A-Za-z_][\w\.]*)\s+AS\s+TIMESTAMP\s*\)",
    re.IGNORECASE,
)


def _translate(sql: str) -> str:
    sql = _PCT_S_RE.sub("?", sql)
    sql = _DBO_RE.sub("", sql)
    out = sqlglot.transpile(sql, read="tsql", write="sqlite")[0]
    out = _CAST_TS_RE.sub(lambda m: f"TRY_PARSE_DATE({m.group(1)})", out)
    return out


class _Cursor:
    def __init__(self, sqlite_cur: sqlite3.Cursor, as_dict: bool):
        self._cur = sqlite_cur
        self._as_dict = as_dict

    def execute(self, sql, params=()):
        translated = _translate(sql)
        self._cur.execute(translated, params or ())
        return self

    def fetchone(self):
        row = self._cur.fetchone()
        if row is None:
            return None
        if self._as_dict:
            cols = [d[0] for d in self._cur.description]
            return dict(zip(cols, row))
        return tuple(row)

    def fetchall(self):
        rows = self._cur.fetchall()
        if self._as_dict:
            cols = [d[0] for d in self._cur.description]
            return [dict(zip(cols, r)) for r in rows]
        return [tuple(r) for r in rows]

    @property
    def rowcount(self):
        return self._cur.rowcount

    def close(self):
        self._cur.close()


class Connection:
    def __init__(self, path: str):
        self._conn = sqlite3.connect(path, check_same_thread=False)
        self._conn.execute("PRAGMA foreign_keys = ON")
        _register_udfs(self._conn)

    def cursor(self, as_dict: bool = False) -> _Cursor:
        return _Cursor(self._conn.cursor(), as_dict)

    def commit(self):
        self._conn.commit()

    def close(self):
        self._conn.close()


def connect(path: str) -> Connection:
    return Connection(path)
