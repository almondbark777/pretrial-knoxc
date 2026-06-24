package db

import (
	"database/sql"
	"strings"
	"time"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/models"
)

// maxChatBody caps a single message so a paste can't bloat the DB or a render.
const maxChatBody = 4000

// AddChatMessage inserts a group-chat line and returns the stored row (with its
// new id + ET timestamp). Author is the sender's email; body is trimmed and
// length-capped. Chat is deliberately NOT written through the audit log — it's
// high-volume and would drown the audit trail.
func AddChatMessage(d *sql.DB, author, body string) (models.ChatMessage, error) {
	author = strings.TrimSpace(author)
	body = strings.TrimSpace(body)
	if author == "" || body == "" {
		return models.ChatMessage{}, errEmptyField
	}
	if len(body) > maxChatBody {
		body = body[:maxChatBody]
	}
	now := compute.NowET().Format(time.RFC3339)
	res, err := d.Exec(`INSERT INTO chat_messages (author, body, created_at) VALUES (?, ?, ?)`, author, body, now)
	if err != nil {
		return models.ChatMessage{}, err
	}
	id, _ := res.LastInsertId()
	return models.ChatMessage{ID: id, Author: author, Body: body, Created: now}, nil
}

// RecentChatMessages returns the most recent `limit` messages in chronological
// order (oldest first) — the backlog a client sees when it first opens the chat.
func RecentChatMessages(d *sql.DB, limit int) ([]models.ChatMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.Query(
		`SELECT msg_id, author, body, IFNULL(created_at,'') FROM chat_messages ORDER BY msg_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := scanChat(rows)
	if err != nil {
		return nil, err
	}
	// scanned newest-first; reverse to chronological.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ChatMessagesSince returns messages with id greater than sinceID, oldest first
// — the catch-up a reconnecting client receives via the SSE Last-Event-ID header.
func ChatMessagesSince(d *sql.DB, sinceID int64, limit int) ([]models.ChatMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.Query(
		`SELECT msg_id, author, body, IFNULL(created_at,'') FROM chat_messages
		   WHERE msg_id > ? ORDER BY msg_id ASC LIMIT ?`, sinceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChat(rows)
}

// PruneChatMessages deletes messages older than `before`. Returns the row count
// removed. Called periodically to enforce the 7-day retention window.
//
// created_at is stored as RFC3339 with an offset (compute.NowET writes e.g.
// "...-04:00" or "...-05:00" across the DST switch). A naive lexicographic
// string compare against an offset-bearing cutoff would mis-order rows whose
// offsets differ. Normalizing both sides to the same UTC "Z" form via strftime
// makes the comparison instant-correct regardless of DST. strftime understands
// the stored offset and converts to UTC; the cutoff is formatted as UTC Z.
func PruneChatMessages(d *sql.DB, before time.Time) (int64, error) {
	cutoff := before.UTC().Format("2006-01-02T15:04:05Z")
	res, err := d.Exec(
		`DELETE FROM chat_messages
		   WHERE strftime('%Y-%m-%dT%H:%M:%SZ', created_at) < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func scanChat(rows *sql.Rows) ([]models.ChatMessage, error) {
	var out []models.ChatMessage
	for rows.Next() {
		var m models.ChatMessage
		if err := rows.Scan(&m.ID, &m.Author, &m.Body, &m.Created); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
