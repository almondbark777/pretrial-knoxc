package handlers

// cache_test.go covers the serve-stale-while-refresh stampede fix (#12):
// - Concurrent stale callers return promptly (the stale snapshot) without
//   blocking each other behind a build mutex.
// - BuildClients runs at most once across a burst of concurrent stale requests.
// - A failed background rebuild keeps the prior snapshot (no error propagation).

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pretrial-knoxc/internal/compute"
)

// makeStaleServer builds a Server whose cache is already expired.
// buildCount tracks how many times the underlying build function is invoked;
// buildErr, if non-nil, makes each build call return that error.
func makeStaleServer(buildCount *atomic.Int64, buildErr error) *Server {
	snap := map[string][]*compute.Client{
		"prior": {{IDN: "prior", Name: "Prior Snapshot", Status: "Open"}},
	}
	s := New(nil, nil, nil, time.Millisecond, false) // 1ms TTL
	s.cached = snap
	s.cachedAt = time.Now().Add(-time.Second) // guaranteed stale
	s.buildClientsFunc = func() (map[string][]*compute.Client, error) {
		buildCount.Add(1)
		// Simulate a build that takes a small amount of time so concurrent
		// goroutines have a chance to overlap.
		time.Sleep(5 * time.Millisecond)
		if buildErr != nil {
			return nil, buildErr
		}
		return map[string][]*compute.Client{
			"fresh": {{IDN: "fresh", Name: "Refreshed", Status: "Open"}},
		}, nil
	}
	return s
}

// TestCacheStampede asserts that N concurrent stale calls all return the prior
// snapshot immediately and trigger exactly one background rebuild.
func TestCacheStampede(t *testing.T) {
	var buildCount atomic.Int64
	s := makeStaleServer(&buildCount, nil)

	const goroutines = 20
	var wg sync.WaitGroup
	results := make([]map[string][]*compute.Client, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cl, err := s.clients()
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", idx, err)
				return
			}
			results[idx] = cl
		}(i)
	}
	wg.Wait()

	// Every goroutine must have gotten the prior (stale) snapshot immediately.
	for i, cl := range results {
		if cl == nil {
			t.Errorf("goroutine %d: got nil", i)
			continue
		}
		if _, ok := cl["prior"]; !ok {
			t.Errorf("goroutine %d: did not get the stale snapshot (got %v)", i, cl)
		}
	}

	// Wait briefly for the background goroutine to complete.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		done := !s.refreshing
		s.mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	got := buildCount.Load()
	if got != 1 {
		t.Errorf("BuildClients called %d times, want exactly 1", got)
	}
}

// TestCacheStaleReturnedPromptly asserts that a stale-hit goroutine does not
// block waiting for the rebuild: it should return before the build finishes.
func TestCacheStaleReturnedPromptly(t *testing.T) {
	var buildCount atomic.Int64
	// Build takes 50ms; the stale call should return well before that.
	snap := map[string][]*compute.Client{
		"prior": {{IDN: "prior"}},
	}
	s := New(nil, nil, nil, time.Millisecond, false)
	s.cached = snap
	s.cachedAt = time.Now().Add(-time.Second)
	s.buildClientsFunc = func() (map[string][]*compute.Client, error) {
		buildCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		return map[string][]*compute.Client{"fresh": {{IDN: "fresh"}}}, nil
	}

	start := time.Now()
	cl, err := s.clients()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cl["prior"]; !ok {
		t.Errorf("stale call did not return the prior snapshot")
	}
	// The stale return must be fast (< 10ms), not wait for the 50ms build.
	if elapsed > 20*time.Millisecond {
		t.Errorf("stale call blocked for %v, want < 20ms", elapsed)
	}
}

// TestCacheFailedRebuildKeepsPrior asserts that when the background rebuild
// errors, the server retains the old snapshot (no degradation to an error state)
// and the refreshing flag is cleared so the next stale hit can retry.
func TestCacheFailedRebuildKeepsPrior(t *testing.T) {
	var buildCount atomic.Int64
	buildErr := errors.New("DB unavailable")
	s := makeStaleServer(&buildCount, buildErr)

	// Trigger a stale call to launch the background rebuild.
	cl, err := s.clients()
	if err != nil {
		t.Fatalf("stale call should not error: %v", err)
	}
	if _, ok := cl["prior"]; !ok {
		t.Errorf("expected prior snapshot, got %v", cl)
	}

	// Wait for the background goroutine to finish.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		done := !s.refreshing
		s.mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The prior snapshot must still be intact.
	s.mu.Lock()
	snap := s.cached
	refreshing := s.refreshing
	s.mu.Unlock()

	if _, ok := snap["prior"]; !ok {
		t.Errorf("failed rebuild should keep prior snapshot, got %v", snap)
	}
	if refreshing {
		t.Error("refreshing flag should be cleared after a failed rebuild")
	}
}

// TestCacheColdStart asserts that a cold start (no snapshot yet) builds
// synchronously and returns the new snapshot, not nil.
func TestCacheColdStart(t *testing.T) {
	var buildCount atomic.Int64
	s := New(nil, nil, nil, time.Minute, false) // long TTL, but no snapshot
	s.buildClientsFunc = func() (map[string][]*compute.Client, error) {
		buildCount.Add(1)
		return map[string][]*compute.Client{
			"fresh": {{IDN: "fresh", Name: "First Build", Status: "Open"}},
		}, nil
	}

	cl, err := s.clients()
	if err != nil {
		t.Fatalf("cold-start build error: %v", err)
	}
	if _, ok := cl["fresh"]; !ok {
		t.Errorf("cold start did not return the new snapshot: %v", cl)
	}
	if buildCount.Load() != 1 {
		t.Errorf("BuildClients called %d times on cold start, want 1", buildCount.Load())
	}
}

// TestBackgroundRefreshDiscardedAfterClear is the stale-write guard (#12 follow-up):
// a rebuild that was in flight when clearCache() ran (e.g. a mutation handler clears
// the cache right after committing) must DISCARD its result instead of overwriting
// the clear — otherwise the just-made add/delete/override is hidden for a full TTL.
// Driven synchronously (clearCache fires from inside the build) so it's deterministic.
func TestBackgroundRefreshDiscardedAfterClear(t *testing.T) {
	s := New(nil, nil, nil, time.Minute, false)
	s.cached = map[string][]*compute.Client{"prior": {{IDN: "prior"}}}
	gen := s.cacheGen
	s.buildClientsFunc = func() (map[string][]*compute.Client, error) {
		// A mutation commits and clears the cache WHILE this build is in flight.
		s.clearCache()
		return map[string][]*compute.Client{"stale-build": {{IDN: "stale-build"}}}, nil
	}
	s.refreshing = true
	s.backgroundRefresh(gen) // gen captured before the clear is now stale

	s.mu.Lock()
	cached, refreshing := s.cached, s.refreshing
	s.mu.Unlock()
	if cached != nil {
		t.Fatalf("in-flight rebuild overwrote a post-mutation clearCache: cached = %v, want nil (discarded)", cached)
	}
	if refreshing {
		t.Error("refreshing should be cleared after backgroundRefresh")
	}
}

// TestClearCacheResetsRefreshing asserts that clearCache resets the refreshing
// flag so a subsequent stale call can trigger a new rebuild.
func TestClearCacheResetsRefreshing(t *testing.T) {
	var buildCount atomic.Int64
	s := makeStaleServer(&buildCount, nil)

	// Simulate the refreshing flag already set (e.g. a rebuild in flight).
	s.mu.Lock()
	s.refreshing = true
	s.mu.Unlock()

	s.clearCache()

	s.mu.Lock()
	cachedNil := s.cached == nil
	refreshing := s.refreshing
	s.mu.Unlock()

	if !cachedNil {
		t.Error("clearCache should nil the cached snapshot")
	}
	if refreshing {
		t.Error("clearCache should clear the refreshing flag")
	}
}
