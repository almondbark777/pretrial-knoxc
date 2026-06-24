package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func freshChatDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := EnsureSchema(d); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return d
}

func TestChatAddRecentAndSince(t *testing.T) {
	d := freshChatDB(t)
	m1, err := AddChatMessage(d, "a@knoxsheriff.org", "first")
	if err != nil {
		t.Fatalf("add1: %v", err)
	}
	m2, err := AddChatMessage(d, "b@knoxsheriff.org", "  second  ") // trimmed
	if err != nil {
		t.Fatalf("add2: %v", err)
	}
	if m2.ID <= m1.ID {
		t.Fatalf("ids not increasing: %d then %d", m1.ID, m2.ID)
	}
	if m2.Body != "second" {
		t.Fatalf("body not trimmed: %q", m2.Body)
	}
	if _, err := AddChatMessage(d, "a@knoxsheriff.org", "   "); err == nil {
		t.Fatalf("empty body should be rejected")
	}

	recent, err := RecentChatMessages(d, 50)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 2 || recent[0].Body != "first" || recent[1].Body != "second" {
		t.Fatalf("recent should be oldest-first [first, second], got %+v", recent)
	}

	since, err := ChatMessagesSince(d, m1.ID, 50)
	if err != nil {
		t.Fatalf("since: %v", err)
	}
	if len(since) != 1 || since[0].ID != m2.ID {
		t.Fatalf("ChatMessagesSince(m1) want [m2], got %+v", since)
	}
}

func TestChatPruneByAge(t *testing.T) {
	d := freshChatDB(t)
	old := time.Now().AddDate(0, 0, -10).Format(time.RFC3339)
	if _, err := d.Exec(`INSERT INTO chat_messages (author, body, created_at) VALUES (?,?,?)`,
		"a@knoxsheriff.org", "stale", old); err != nil {
		t.Fatalf("insert old: %v", err)
	}
	if _, err := AddChatMessage(d, "b@knoxsheriff.org", "fresh"); err != nil {
		t.Fatalf("add fresh: %v", err)
	}
	n, err := PruneChatMessages(d, time.Now().AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("prune removed %d, want 1 (only the 10-day-old row)", n)
	}
	recent, _ := RecentChatMessages(d, 50)
	if len(recent) != 1 || recent[0].Body != "fresh" {
		t.Fatalf("after prune want [fresh], got %+v", recent)
	}
}

// TestChatPruneDSTBoundary exercises the case a naive lexicographic compare gets
// wrong: two rows whose stored RFC3339 offsets differ (one in EDT -04:00, one in
// EST -05:00) straddling the cutoff. The strftime-to-UTC normalization must keep
// the comparison instant-correct so exactly the older-in-real-time row is pruned.
func TestChatPruneDSTBoundary(t *testing.T) {
	d := freshChatDB(t)

	// Two instants 30 minutes apart in real (UTC) time, written with DIFFERENT
	// local offsets so their raw RFC3339 strings sort the WRONG way lexically:
	//   earlier : 2025-11-02T01:50:00-04:00  == 05:50:00Z (EDT, before the fall-back)
	//   later   : 2025-11-02T01:20:00-05:00  == 06:20:00Z (EST, after the fall-back)
	// Lexically "01:50...-04:00" > "01:20...-05:00", i.e. reversed from real order.
	earlier := "2025-11-02T01:50:00-04:00" // 05:50Z — should be pruned
	later := "2025-11-02T01:20:00-05:00"   // 06:20Z — should survive
	for _, ts := range []string{earlier, later} {
		if _, err := d.Exec(
			`INSERT INTO chat_messages (author, body, created_at) VALUES (?,?,?)`,
			"a@knoxsheriff.org", ts, ts); err != nil {
			t.Fatalf("insert %s: %v", ts, err)
		}
	}

	// Cutoff at 06:00Z: prunes the 05:50Z row, keeps the 06:20Z row. A naive
	// string compare would prune the wrong one (or neither).
	cutoff := time.Date(2025, 11, 2, 6, 0, 0, 0, time.UTC)
	n, err := PruneChatMessages(d, cutoff)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("prune removed %d, want exactly 1 (the 05:50Z row)", n)
	}
	recent, _ := RecentChatMessages(d, 50)
	if len(recent) != 1 || recent[0].Body != later {
		t.Fatalf("after DST prune want [%s], got %+v", later, recent)
	}
}
