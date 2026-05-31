// Package metrics provides dependency-free request instrumentation exposed in
// Prometheus text exposition format at /metrics. Netdata (or any Prometheus
// scraper) reads it on the same box; the app listens only on 127.0.0.1, so the
// endpoint is never publicly reachable.
//
// No external client library — we emit the handful of series that actually drive
// tuning decisions (request rate, latency, errors per route, Go runtime/GC) by
// hand. This keeps the single-binary, no-CGO, minimal-deps ethos intact.
package metrics

import (
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// latency histogram buckets in seconds (cumulative, Prometheus-style "le").
// Fixed array so len(buckets) is a compile-time constant (used as an array size).
var buckets = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// routeStat accumulates counters for one (route, method) pair.
type routeStat struct {
	count      uint64                   // total requests
	errors     uint64                   // 5xx responses
	clientErrs uint64                   // 4xx responses
	sumSeconds float64                  // sum of durations (for avg)
	bucketHits [len(buckets) + 1]uint64 // last slot = +Inf overflow
}

// Metrics is a concurrency-safe registry of per-route HTTP stats.
type Metrics struct {
	mu       sync.Mutex
	stats    map[string]*routeStat // key: method + " " + route pattern
	inFlight int64
	started  time.Time
}

// New returns a registry. start is the process start time (passed in rather than
// captured here so the caller controls it; time.Now is fine in main).
func New(start time.Time) *Metrics {
	return &Metrics{stats: make(map[string]*routeStat), started: start}
}

// Middleware records count, in-flight, latency, and status class for each request.
// It reads chi's matched route pattern AFTER the handler runs, so high-cardinality
// raw paths (e.g. ?idn=...) collapse to the registered pattern.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		m.mu.Lock()
		m.inFlight++
		m.mu.Unlock()

		start := time.Now()
		next.ServeHTTP(ww, r)
		elapsed := time.Since(start).Seconds()

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			// No matched pattern: a 404, or a request the auth middleware
			// short-circuited (401/redirect) before chi reached routeHTTP.
			// Bucket as "other" so raw paths never become metric labels.
			route = "other"
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		m.mu.Lock()
		m.inFlight--
		key := r.Method + " " + route
		s := m.stats[key]
		if s == nil {
			s = &routeStat{}
			m.stats[key] = s
		}
		s.count++
		s.sumSeconds += elapsed
		switch {
		case status >= 500:
			s.errors++
		case status >= 400:
			s.clientErrs++
		}
		for i, b := range buckets {
			if elapsed <= b {
				s.bucketHits[i]++
			}
		}
		s.bucketHits[len(buckets)]++ // +Inf always counts
		m.mu.Unlock()
	})
}

// Handler writes the registry in Prometheus text exposition format.
func (m *Metrics) Handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	// Snapshot under lock, format outside.
	type row struct {
		method, route string
		s             routeStat
	}
	rows := make([]row, 0, len(m.stats))
	for k, s := range m.stats {
		method, route := splitKey(k)
		rows = append(rows, row{method, route, *s})
	}
	inFlight := m.inFlight
	uptime := time.Since(m.started).Seconds()
	m.mu.Unlock()

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].route != rows[j].route {
			return rows[i].route < rows[j].route
		}
		return rows[i].method < rows[j].method
	})

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	fmt.Fprintln(w, "# HELP ptr_http_requests_total Total HTTP requests by route, method, status class.")
	fmt.Fprintln(w, "# TYPE ptr_http_requests_total counter")
	for _, rw := range rows {
		lbl := fmt.Sprintf(`route=%q,method=%q`, rw.route, rw.method)
		ok := rw.s.count - rw.s.errors - rw.s.clientErrs
		writeLine(w, "ptr_http_requests_total", lbl+`,status="2xx_3xx"`, float64(ok))
		writeLine(w, "ptr_http_requests_total", lbl+`,status="4xx"`, float64(rw.s.clientErrs))
		writeLine(w, "ptr_http_requests_total", lbl+`,status="5xx"`, float64(rw.s.errors))
	}

	fmt.Fprintln(w, "# HELP ptr_http_request_duration_seconds Request latency histogram.")
	fmt.Fprintln(w, "# TYPE ptr_http_request_duration_seconds histogram")
	for _, rw := range rows {
		base := fmt.Sprintf(`route=%q,method=%q`, rw.route, rw.method)
		for i, b := range buckets {
			le := strconv.FormatFloat(b, 'g', -1, 64)
			writeLine(w, "ptr_http_request_duration_seconds_bucket", base+`,le="`+le+`"`, float64(rw.s.bucketHits[i]))
		}
		writeLine(w, "ptr_http_request_duration_seconds_bucket", base+`,le="+Inf"`, float64(rw.s.bucketHits[len(buckets)]))
		writeLine(w, "ptr_http_request_duration_seconds_sum", base, rw.s.sumSeconds)
		writeLine(w, "ptr_http_request_duration_seconds_count", base, float64(rw.s.count))
	}

	fmt.Fprintln(w, "# HELP ptr_http_requests_in_flight Requests currently being served.")
	fmt.Fprintln(w, "# TYPE ptr_http_requests_in_flight gauge")
	writeLine(w, "ptr_http_requests_in_flight", "", float64(inFlight))

	// Go runtime — goroutines, memory, GC. The cheap early-warning signals for
	// leaks and GC pressure on a long-running single binary.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintln(w, "# HELP ptr_process_uptime_seconds Seconds since the server started.")
	fmt.Fprintln(w, "# TYPE ptr_process_uptime_seconds gauge")
	writeLine(w, "ptr_process_uptime_seconds", "", uptime)
	fmt.Fprintln(w, "# TYPE ptr_go_goroutines gauge")
	writeLine(w, "ptr_go_goroutines", "", float64(runtime.NumGoroutine()))
	fmt.Fprintln(w, "# TYPE ptr_go_memory_alloc_bytes gauge")
	writeLine(w, "ptr_go_memory_alloc_bytes", "", float64(ms.Alloc))
	fmt.Fprintln(w, "# TYPE ptr_go_memory_sys_bytes gauge")
	writeLine(w, "ptr_go_memory_sys_bytes", "", float64(ms.Sys))
	fmt.Fprintln(w, "# TYPE ptr_go_gc_total counter")
	writeLine(w, "ptr_go_gc_total", "", float64(ms.NumGC))
}

func writeLine(w http.ResponseWriter, name, labels string, v float64) {
	if labels == "" {
		fmt.Fprintf(w, "%s %s\n", name, strconv.FormatFloat(v, 'g', -1, 64))
		return
	}
	fmt.Fprintf(w, "%s{%s} %s\n", name, labels, strconv.FormatFloat(v, 'g', -1, 64))
}

// splitKey splits "METHOD route" back into its parts (route may contain spaces in
// theory, but chi patterns don't, so split on the first space only).
func splitKey(k string) (method, route string) {
	for i := 0; i < len(k); i++ {
		if k[i] == ' ' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}
