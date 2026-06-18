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
