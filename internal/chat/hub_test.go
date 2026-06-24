package chat

import (
	"testing"
	"time"
)

func recv(t *testing.T, c <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-c:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hub event")
		return Event{}
	}
}

func TestHubPresenceAndFanout(t *testing.T) {
	h := NewHub()

	// First subscriber gets a presence snapshot listing itself.
	a := h.Subscribe("alice@knoxsheriff.org")
	ev := recv(t, a.C)
	if ev.Type != "presence" {
		t.Fatalf("alice first event = %q, want presence", ev.Type)
	}
	if online, _ := ev.Data.([]string); len(online) != 1 || online[0] != "alice@knoxsheriff.org" {
		t.Fatalf("presence = %v, want [alice]", ev.Data)
	}

	// Second subscriber → existing client sees the count rise to 2.
	b := h.Subscribe("bob@knoxsheriff.org")
	if online, _ := recv(t, a.C).Data.([]string); len(online) != 2 {
		t.Fatalf("after bob joins, alice presence = %v, want 2 online", online)
	}
	if recv(t, b.C).Type != "presence" {
		t.Fatalf("bob's first event should be a presence snapshot")
	}

	// A published message reaches both clients.
	h.Publish(Event{Type: "msg", ID: 7, Data: "hi"})
	for name, c := range map[string]*Client{"alice": a, "bob": b} {
		if got := recv(t, c.C); got.Type != "msg" || got.ID != 7 {
			t.Fatalf("%s should receive msg id=7, got %+v", name, got)
		}
	}

	// Unsubscribing alice closes her channel and drops her from presence.
	h.Unsubscribe(a)
	if online, _ := recv(t, b.C).Data.([]string); len(online) != 1 || online[0] != "bob@knoxsheriff.org" {
		t.Fatalf("after alice leaves, bob presence = %v, want [bob]", online)
	}
	if _, ok := <-a.C; ok {
		t.Fatal("alice's channel should be closed after Unsubscribe")
	}
}

// TestFanoutDoesNotBlockOnStuckClient verifies a client that never drains its
// channel cannot wedge the hub: fanout drops to it (default branch) so other
// clients keep receiving and Publish never blocks.
func TestFanoutDoesNotBlockOnStuckClient(t *testing.T) {
	h := NewHub()

	// "stuck" never reads its channel; "live" does.
	stuck := h.Subscribe("stuck@knoxsheriff.org")
	recv(t, stuck.C) // its own presence snapshot (the only one we ever drain)
	live := h.Subscribe("live@knoxsheriff.org")
	recv(t, live.C) // live's own presence snapshot (on register)

	// Publish more events than the per-client buffer (32) can hold. Without the
	// non-blocking fanout this would deadlock once stuck's buffer fills.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			h.Publish(Event{Type: "msg", ID: int64(i), Data: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a stuck (non-draining) client")
	}

	// The live client still gets fresh events.
	got := recv(t, live.C)
	if got.Type != "msg" {
		t.Fatalf("live client should still receive msgs, got %+v", got)
	}
}
