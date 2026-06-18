// Package chat is the in-memory pub/sub hub behind the group chat: it tracks who
// has an open Server-Sent-Events stream (presence) and fans new messages out to
// every connected client. State is per-process and intentionally ephemeral — the
// message history itself lives in SQLite (internal/db/chat.go); the hub only
// moves live events. Sized for a single box and a couple dozen users.
package chat

import "sort"

// Event is one server→client item on the SSE stream. Type is "msg" (a chat line,
// ID = its DB id for Last-Event-ID catch-up) or "presence" (Data is []string of
// the emails currently online).
type Event struct {
	Type string
	ID   int64
	Data any
}

// Client is one open SSE connection. The stream handler reads C until the hub
// closes it (on Unsubscribe) or the request context ends.
type Client struct {
	Email string
	C     chan Event
}

// Hub fans events out to all connected clients and tracks presence by email
// (a user may have several tabs open — presence is a per-email connection count).
type Hub struct {
	register   chan *Client
	unregister chan *Client
	publish    chan Event
	clients    map[*Client]bool
	presence   map[string]int
}

// NewHub returns a started hub (its run loop owns all mutable state, so no locks).
func NewHub() *Hub {
	h := &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		publish:    make(chan Event, 64),
		clients:    map[*Client]bool{},
		presence:   map[string]int{},
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	for {
		select {
		case cl := <-h.register:
			h.clients[cl] = true
			h.presence[cl.Email]++
			h.broadcastPresence()
		case cl := <-h.unregister:
			if h.clients[cl] {
				delete(h.clients, cl)
				close(cl.C)
				if h.presence[cl.Email] > 0 {
					h.presence[cl.Email]--
					if h.presence[cl.Email] == 0 {
						delete(h.presence, cl.Email)
					}
				}
				h.broadcastPresence()
			}
		case ev := <-h.publish:
			h.fanout(ev)
		}
	}
}

func (h *Hub) fanout(ev Event) {
	for cl := range h.clients {
		select {
		case cl.C <- ev:
		default:
			// Slow/stuck client: drop rather than block the whole hub. The client
			// catches up on reconnect via Last-Event-ID (messages) and gets a fresh
			// presence snapshot, so a dropped event is self-healing.
		}
	}
}

func (h *Hub) broadcastPresence() {
	online := make([]string, 0, len(h.presence))
	for e := range h.presence {
		online = append(online, e)
	}
	sort.Strings(online)
	h.fanout(Event{Type: "presence", Data: online})
}

// Subscribe registers a new connection for the given user email and returns its
// Client. Pair every Subscribe with a deferred Unsubscribe.
func (h *Hub) Subscribe(email string) *Client {
	cl := &Client{Email: email, C: make(chan Event, 32)}
	h.register <- cl
	return cl
}

// Unsubscribe removes a connection and closes its channel. Safe to call once per
// Client; the run loop guards against double-unregister.
func (h *Hub) Unsubscribe(cl *Client) { h.unregister <- cl }

// Publish fans an event (typically a new message) out to all connected clients.
func (h *Hub) Publish(ev Event) { h.publish <- ev }
