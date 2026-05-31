package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestMiddlewareAndHandler(t *testing.T) {
	m := New(time.Now())
	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Get("/api/clients", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	r.Get("/boom", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })

	// Drive a few requests through known routes.
	for i := 0; i < 3; i++ {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/clients?idn=999", nil))
	}
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/boom", nil))

	rec := httptest.NewRecorder()
	m.Handler(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	// Raw query string must collapse to the route pattern (no high cardinality).
	if strings.Contains(body, "idn=999") {
		t.Fatalf("metrics leaked raw query param:\n%s", body)
	}
	if !strings.Contains(body, `route="/api/clients"`) {
		t.Fatalf("missing /api/clients route series:\n%s", body)
	}
	// 3 successful GETs to /api/clients.
	if !strings.Contains(body, `ptr_http_requests_total{route="/api/clients",method="GET",status="2xx_3xx"} 3`) {
		t.Fatalf("expected 3 ok requests:\n%s", body)
	}
	// 1 server error to /boom.
	if !strings.Contains(body, `ptr_http_requests_total{route="/boom",method="GET",status="5xx"} 1`) {
		t.Fatalf("expected 1 5xx:\n%s", body)
	}
	// Histogram count line present.
	if !strings.Contains(body, `ptr_http_request_duration_seconds_count{route="/api/clients",method="GET"} 3`) {
		t.Fatalf("missing histogram count:\n%s", body)
	}
	// Runtime series present.
	for _, want := range []string{"ptr_go_goroutines", "ptr_go_memory_alloc_bytes", "ptr_process_uptime_seconds"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing runtime series %q:\n%s", want, body)
		}
	}
}
