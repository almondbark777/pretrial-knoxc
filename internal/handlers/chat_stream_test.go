package handlers

import (
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/chat"
	"pretrial-knoxc/internal/db"
)

// chatTestServer builds a Server wired with a fresh DB (chat schema), an
// Authenticator (for the CSRF frame), and a live hub — enough for ChatStream.
func chatTestServer(t *testing.T) *Server {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.EnsureSchema(d); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	a := auth.New("pw", "secret-1234567890123456789012345678", nil, nil, nil)
	s := New(d, a, nil, time.Minute, false)
	s.Chat = chat.NewHub()
	return s
}

// blockingRW is an http.ResponseWriter whose Write blocks until the write
// deadline set via SetWriteDeadline elapses, then fails with a timeout — the
// behavior of a real socket whose TCP send window is full and never drains.
// It implements the methods http.NewResponseController unwraps for Flush and
// SetWriteDeadline, so the handler's per-batch deadline takes effect.
type blockingRW struct {
	mu       sync.Mutex
	hdr      http.Header
	deadline time.Time
	writes   int
}

func (b *blockingRW) Header() http.Header {
	if b.hdr == nil {
		b.hdr = http.Header{}
	}
	return b.hdr
}

func (b *blockingRW) WriteHeader(int) {}

func (b *blockingRW) Write(p []byte) (int, error) {
	b.mu.Lock()
	dl := b.deadline
	b.writes++
	n := b.writes
	b.mu.Unlock()
	// First write (the CSRF frame) succeeds so the stream gets established; every
	// subsequent write simulates a wedged socket: block until the deadline, then
	// time out.
	if n == 1 {
		return len(p), nil
	}
	if dl.IsZero() {
		// No deadline armed: a real wedged socket would block forever. Fail loudly
		// rather than hang the test.
		return 0, &timeoutErr{}
	}
	if d := time.Until(dl); d > 0 {
		time.Sleep(d)
	}
	return 0, &timeoutErr{}
}

func (b *blockingRW) Flush() {}

func (b *blockingRW) SetWriteDeadline(t time.Time) error {
	b.mu.Lock()
	b.deadline = t
	b.mu.Unlock()
	return nil
}

type timeoutErr struct{}

func (*timeoutErr) Error() string   { return "i/o timeout" }
func (*timeoutErr) Timeout() bool   { return true }
func (*timeoutErr) Temporary() bool { return true }

// TestChatStreamReturnsOnBlockedWriter checks that a stream whose client never
// drains (its writes time out under the write deadline) returns from the handler
// within that deadline, instead of hanging until the TCP timeout. The deferred
// Unsubscribe then frees the goroutine and channel.
func TestChatStreamReturnsOnBlockedWriter(t *testing.T) {
	s := chatTestServer(t)

	// Seed enough backlog that the handler attempts a second (blocking) write.
	for i := 0; i < 3; i++ {
		if _, err := db.AddChatMessage(s.DB, "a@knoxsheriff.org", "hello"); err != nil {
			t.Fatalf("seed chat: %v", err)
		}
	}

	w := &blockingRW{}
	r, err := http.NewRequest(http.MethodGet, "/chat/stream", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	done := make(chan struct{})
	go func() {
		s.ChatStream(w, r)
		close(done)
	}()

	// 5s deadline + generous slack; without the deadline this never returns.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ChatStream did not return on a blocked (non-draining) writer")
	}
}
