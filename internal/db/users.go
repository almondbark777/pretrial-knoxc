// users.go is the in-app roles & permissions layer (migration 010). app_users is
// the source of truth for who may sign in and at what tier (officer <
// supervisor < admin); an Admin edits it from /console/admin with no redeploy.
//
// The auth package consults this table through a RoleCache (a short-TTL snapshot,
// so role resolution doesn't hit the DB on every request) and treats it as
// authoritative once seeded. The env ALLOWED_EMAILS / SUPERVISOR_EMAILS lists are
// used only to seed an empty table (SeedUsersIfEmpty) and as a fail-safe fallback
// if the lookup ever errors. A hardcoded break-glass admin in the auth package
// is always admin regardless of this table, so the owner can never be locked out.
package db

import (
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/models"
)

const errBadRole = adminErr("role must be officer, supervisor, or admin")

// ValidRole reports whether r is one of the three tiers.
func ValidRole(r string) bool {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case "officer", "supervisor", "admin":
		return true
	}
	return false
}

// roleRank orders the roster admin → supervisor → officer for display.
func roleRank(r string) int {
	switch r {
	case "admin":
		return 0
	case "supervisor":
		return 1
	default:
		return 2
	}
}

// ListAppUsers returns the roster sorted by tier (admin first) then email, for the
// admin Users & Roles panel. Tolerant of a DB predating migration 010.
func ListAppUsers(d *sql.DB) ([]models.AppUser, error) {
	if !tableExists(d, "app_users") {
		return nil, nil
	}
	rows, err := d.Query(`SELECT email, role, IFNULL(added_by,''), IFNULL(created_at,''), IFNULL(updated_at,'') FROM app_users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AppUser
	for rows.Next() {
		var u models.AppUser
		if err := rows.Scan(&u.Email, &u.Role, &u.AddedBy, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := roleRank(out[i].Role), roleRank(out[j].Role)
		if ri != rj {
			return ri < rj
		}
		return out[i].Email < out[j].Email
	})
	return out, nil
}

// LoadUserRoles returns email(lower)→role for every valid app_users row. Returns an
// empty map (not an error) when the table is absent.
func LoadUserRoles(d *sql.DB) (map[string]string, error) {
	out := map[string]string{}
	if !tableExists(d, "app_users") {
		return out, nil
	}
	rows, err := d.Query("SELECT email, role FROM app_users")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var email, role string
		if err := rows.Scan(&email, &role); err != nil {
			return out, err
		}
		email = strings.ToLower(strings.TrimSpace(email))
		role = strings.ToLower(strings.TrimSpace(role))
		if email == "" || !ValidRole(role) {
			continue
		}
		out[email] = role
	}
	return out, rows.Err()
}

// SetUserRole adds or re-roles a user (upsert) and audits old→new. added_by /
// created_at are preserved on an existing row.
func SetUserRole(d *sql.DB, email, role, by string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	role = strings.ToLower(strings.TrimSpace(role))
	if email == "" {
		return errEmptyField
	}
	if !ValidRole(role) {
		return errBadRole
	}
	var old sql.NullString
	_ = d.QueryRow("SELECT role FROM app_users WHERE email = ?", email).Scan(&old)
	now := compute.NowET().Format("2006-01-02 15:04:05 MST")
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "user_role_set", Table: "app_users", RowID: email, OldValue: old.String, NewValue: role},
		`INSERT INTO app_users (email, role, added_by, created_at, updated_at)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT(email) DO UPDATE SET role = excluded.role, updated_at = excluded.updated_at`,
		email, role, nz(by), now, now)
}

// RemoveUser revokes a user (deletes their row) and audits it.
func RemoveUser(d *sql.DB, email, by string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return errEmptyField
	}
	var old sql.NullString
	_ = d.QueryRow("SELECT role FROM app_users WHERE email = ?", email).Scan(&old)
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "user_remove", Table: "app_users", RowID: email, OldValue: old.String},
		`DELETE FROM app_users WHERE email = ?`, email)
}

// SeedUsersIfEmpty populates app_users from the env allow-lists the FIRST time only
// (no-op once the table has any row), so an upgraded deployment shows its existing
// roster at the right tiers and the admin can take it from there. Higher roles are
// applied last so a person on multiple lists lands on their highest tier.
func SeedUsersIfEmpty(d *sql.DB, allowed, supervisors, admins []string) error {
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM app_users").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	roles := map[string]string{}
	addAll := func(list []string, role string) {
		for _, e := range list {
			if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
				roles[e] = role
			}
		}
	}
	addAll(allowed, "officer")
	addAll(supervisors, "supervisor")
	addAll(admins, "admin")
	if len(roles) == 0 {
		return nil
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := compute.NowET().Format("2006-01-02 15:04:05 MST")
	for e, r := range roles {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO app_users (email, role, added_by, created_at, updated_at) VALUES (?,?,?,?,?)`,
			e, r, "system (seed)", now, now); err != nil {
			return err
		}
	}
	if err := WriteAudit(tx, AuditEvent{
		User: "system", Action: "users_seed", Table: "app_users",
		NewValue: "seeded " + strconv.Itoa(len(roles)) + " users from env",
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ── RoleCache ────────────────────────────────────────────────────────────────

// RoleCache is a short-TTL snapshot of app_users so the auth middleware can resolve
// a caller's role without a DB query per request. RoleOf reports (role, dbOK):
// dbOK is true whenever the snapshot is usable (role may be "" for an unknown
// email — that's an authoritative "no access"); dbOK is false only if the table
// could never be loaded, telling auth to fall back to the env lists.
type RoleCache struct {
	d   *sql.DB
	ttl time.Duration

	mu     sync.Mutex
	roles  map[string]string
	loaded bool
	exp    time.Time
}

// NewRoleCache builds a cache over app_users with the given freshness window.
func NewRoleCache(d *sql.DB, ttl time.Duration) *RoleCache {
	return &RoleCache{d: d, ttl: ttl}
}

// RoleOf returns the cached role for email, refreshing the snapshot when stale.
func (c *RoleCache) RoleOf(email string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded || time.Now().After(c.exp) {
		if m, err := LoadUserRoles(c.d); err == nil {
			c.roles = m
			c.loaded = true
			c.exp = time.Now().Add(c.ttl)
		} else if !c.loaded {
			return "", false // never loaded → auth uses the env fallback
		}
		// else: a refresh failed but we have a prior snapshot — serve it (stale-OK).
	}
	return c.roles[email], true
}

// Invalidate forces the next RoleOf to reload (called after a role write so changes
// take effect immediately rather than at the next TTL tick).
func (c *RoleCache) Invalidate() {
	c.mu.Lock()
	c.exp = time.Time{}
	c.mu.Unlock()
}
