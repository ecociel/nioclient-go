package nioclient

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type countingFetcher struct {
	mu      sync.Mutex
	calls   int
	session *ResolvedSession
	err     error
}

func (f *countingFetcher) fetch(_ context.Context, _ string) (*ResolvedSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.session == nil {
		return nil, nil
	}
	s := *f.session
	return &s, nil
}

func (f *countingFetcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *countingFetcher) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func sessionValidFor(mins int) *ResolvedSession {
	return &ResolvedSession{
		Principal: "11111111-1111-1111-1111-111111111111",
		ExpiresAt: time.Now().Add(time.Duration(mins) * time.Minute),
	}
}

func testCfg() ResolverConfig {
	return ResolverConfig{
		Capacity:     100,
		L1TTL:        30 * time.Second,
		NegTTL:       2 * time.Second,
		StaleIfError: 0,
	}
}

func TestTokenHashIsHexSha256(t *testing.T) {
	// sha256("") — a known vector.
	if got := TokenHash(""); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("TokenHash(\"\") = %q", got)
	}
	if got := TokenHash("abc"); len(got) != 64 {
		t.Fatalf("TokenHash(\"abc\") len = %d, want 64", len(got))
	}
}

func TestHitDoesNotRefetch(t *testing.T) {
	f := &countingFetcher{session: sessionValidFor(120)}
	r := newCachedResolver(f, testCfg())

	first, err := r.resolve("deadbeef")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if first == nil {
		t.Fatal("expected a session")
	}
	_, _ = r.resolve("deadbeef")
	_, _ = r.resolve("deadbeef")
	if f.count() != 1 {
		t.Fatalf("L1 hit must not refetch: calls = %d", f.count())
	}
}

func TestUnknownTokenIsTombstoned(t *testing.T) {
	f := &countingFetcher{session: nil}
	r := newCachedResolver(f, testCfg())

	if s, _ := r.resolve("nope"); s != nil {
		t.Fatal("expected not_found (nil)")
	}
	if s, _ := r.resolve("nope"); s != nil {
		t.Fatal("expected not_found (nil)")
	}
	if f.count() != 1 {
		t.Fatalf("tombstone must not refetch: calls = %d", f.count())
	}
}

func TestEvictForcesRefetch(t *testing.T) {
	f := &countingFetcher{session: sessionValidFor(120)}
	r := newCachedResolver(f, testCfg())

	_, _ = r.resolve("k")
	r.evict("k")
	_, _ = r.resolve("k")
	if f.count() != 2 {
		t.Fatalf("evict must force a refetch: calls = %d", f.count())
	}
}

func TestExpiredSessionIsForcedMiss(t *testing.T) {
	// Session already expired: every lookup must be a forced miss.
	f := &countingFetcher{session: sessionValidFor(-1)}
	r := newCachedResolver(f, testCfg())

	_, _ = r.resolve("k")
	_, _ = r.resolve("k")
	if f.count() != 2 {
		t.Fatalf("expired entry must not serve from L1: calls = %d", f.count())
	}
}

func TestTransportErrorServesStale(t *testing.T) {
	cfg := testCfg()
	cfg.L1TTL = time.Millisecond // fresh window elapses almost immediately
	cfg.StaleIfError = 5 * time.Second
	f := &countingFetcher{session: sessionValidFor(120)}
	r := newCachedResolver(f, cfg)

	if _, err := r.resolve("k"); err != nil {
		t.Fatalf("prime: %v", err)
	}
	time.Sleep(10 * time.Millisecond) // past freshUntil, within staleUntil

	f.setErr(&resolveError{transport: true, err: errors.New("unavailable")})
	got, err := r.resolve("k")
	if err != nil {
		t.Fatalf("transport error should serve stale, got err: %v", err)
	}
	if got == nil {
		t.Fatal("expected stale session, got nil")
	}
}

func TestBackendErrorPropagates(t *testing.T) {
	cfg := testCfg()
	cfg.L1TTL = time.Millisecond
	cfg.StaleIfError = 5 * time.Second
	f := &countingFetcher{session: sessionValidFor(120)}
	r := newCachedResolver(f, cfg)

	if _, err := r.resolve("k"); err != nil {
		t.Fatalf("prime: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	f.setErr(&resolveError{transport: false, err: errors.New("boom")})
	if _, err := r.resolve("k"); err == nil {
		t.Fatal("backend (non-transport) error must propagate, not serve stale")
	}
}
