package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pretrial-knoxc/internal/auth"
	"pretrial-knoxc/internal/chat"
	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/db"
	"pretrial-knoxc/internal/models"
)

// chatWireMsg is the JSON shape the browser receives for one message on the SSE
// stream (data: <json>). Author is the display name; Self marks the viewer's own
// lines for right-alignment.
type chatWireMsg struct {
	Type   string `json:"type"`
	ID     int64  `json:"id"`
	Author string `json:"author"`
	Email  string `json:"email"`
	Body   string `json:"body"`
	Time   string `json:"time"`
	Self   bool   `json:"self"`
}

func chatMsgJSON(m models.ChatMessage, me string) string {
	disp := ""
	if t, err := time.Parse(time.RFC3339, m.Created); err == nil {
		disp = t.Format("Mon 3:04 PM")
	}
	b, _ := json.Marshal(chatWireMsg{
		Type: "msg", ID: m.ID, Author: compute.FmtOfficer(m.Author),
		Email: strings.ToLower(m.Author), Body: m.Body, Time: disp,
		Self: strings.EqualFold(m.Author, me),
	})
	return string(b)
}

func chatPresenceJSON(online []string) string {
	b, _ := json.Marshal(map[string]any{"type": "presence", "online": online})
	return string(b)
}

func chatCSRFJSON(token string) string {
	b, _ := json.Marshal(map[string]any{"type": "csrf", "token": token})
	return string(b)
}

// writeSSE writes one Server-Sent-Events frame. A positive id sets the SSE id:
// field so a reconnecting EventSource can resume via Last-Event-ID.
func writeSSE(w http.ResponseWriter, id int64, data string) {
	if id > 0 {
		fmt.Fprintf(w, "id: %d\n", id)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// ChatStream is the SSE endpoint (GET /chat/stream): it sends a CSRF token, the
// recent backlog (or a Last-Event-ID catch-up on reconnect), then streams live
// messages + presence until the client disconnects. A 20s heartbeat keeps the
// connection alive through the Cloudflare tunnel.
func (s *Server) ChatStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	me := strings.ToLower(strings.TrimSpace(auth.User(r)))

	// Mint/read the CSRF token BEFORE writing any body (CSRF may Set-Cookie). The
	// client echoes it on POST /chat/send.
	token := s.Auth.CSRF(w, r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // belt-and-suspenders vs proxy buffering

	cl := s.Chat.Subscribe(me)
	defer s.Chat.Unsubscribe(cl)

	writeSSE(w, 0, chatCSRFJSON(token))

	var backlog []models.ChatMessage
	if last := strings.TrimSpace(r.Header.Get("Last-Event-ID")); last != "" {
		if id, err := strconv.ParseInt(last, 10, 64); err == nil {
			backlog, _ = db.ChatMessagesSince(s.DB, id, 200)
		}
	} else {
		backlog, _ = db.RecentChatMessages(s.DB, 50)
	}
	for _, m := range backlog {
		writeSSE(w, m.ID, chatMsgJSON(m, me))
	}
	flusher.Flush()

	ctx := r.Context()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case ev, ok := <-cl.C:
			if !ok {
				return
			}
			switch ev.Type {
			case "msg":
				if m, ok := ev.Data.(models.ChatMessage); ok {
					writeSSE(w, m.ID, chatMsgJSON(m, me))
				}
			case "presence":
				if online, ok := ev.Data.([]string); ok {
					writeSSE(w, 0, chatPresenceJSON(online))
				}
			}
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// ChatSend (POST /chat/send, CSRF-guarded) stores a message and fans it out to
// every connected client (including the sender, which is how their own line
// appears — the client does not echo optimistically).
func (s *Server) ChatSend(w http.ResponseWriter, r *http.Request) {
	me := strings.ToLower(strings.TrimSpace(auth.User(r)))
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		http.Error(w, "empty message", http.StatusBadRequest)
		return
	}
	m, err := db.AddChatMessage(s.DB, me, body)
	if err != nil {
		http.Error(w, "could not save message", http.StatusInternalServerError)
		return
	}
	s.Chat.Publish(chat.Event{Type: "msg", ID: m.ID, Data: m})
	w.WriteHeader(http.StatusNoContent)
}
