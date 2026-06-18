package db

import (
	"database/sql"
	"fmt"
	"strconv"
)

// ListUserTables returns the names of all non-internal tables in the database,
// for the supervisor "export all data" dump. sqlite_* system tables and the
// session store are skipped.
func ListUserTables(d *sql.DB) ([]string, error) {
	rows, err := d.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if n == "sessions" { // session store, if present — not data
			continue
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DumpTable reads an entire table and returns its column names plus every row as
// strings (NULL → ""), for a generic CSV/Excel export. Table sizes here are small
// (low thousands of rows), so loading one table into memory at a time is fine.
// The table name comes from sqlite_master (trusted) and is quoted defensively.
func DumpTable(d *sql.DB, table string) ([]string, [][]string, error) {
	rows, err := d.Query(`SELECT * FROM "` + table + `"`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		rec := make([]string, len(cols))
		for i, v := range vals {
			rec[i] = cellString(v)
		}
		out = append(out, rec)
	}
	return cols, out, rows.Err()
}

// cellString renders a dynamically-scanned SQLite value as a plain string for CSV.
func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", t)
	}
}
