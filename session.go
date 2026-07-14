package nioclient

// Token -> principal resolution with an L1 cache tier — the Go port of the
// normative resolver in nio's check_client/src/session.rs (issue #243/#245).
//
// An opaque session token is hashed in-process (sha256, hex) — the raw token
// never leaves the process — and resolved to {principal, tenant_id, expires_at}
// over am.SessionService on nio-client. The cache is LRU-bounded with a positive
// TTL carrying downward-only jitter (so the TTL is a hard staleness/revocation
// cap), negative tombstones for unknown tokens, single-flight coalescing of
// concurrent misses, refresh-ahead for hot entries, and an opt-in
// stale-if-error window.

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"

	proto "github.com/ecociel/nioclient-go/proto"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// resolveTimeout bounds a single fill's gRPC call. The fill runs on a detached
// context so one caller cancelling does not poison coalesced waiters.
const resolveTimeout = 5 * time.Second

// TokenHash returns hex(sha256(rawToken)) — the 64-char lowercase cache/wire
// key. The raw token is never stored or transmitted; only this hash is.
func TokenHash(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

// ResolvedSession is a resolved session: the principal UUID, its tenant, and
// the wall-clock instant the session stops being valid.
type ResolvedSession struct {
	Principal string
	TenantId  string
	ExpiresAt time.Time
}

// resolveError wraps a resolve fault. transport marks the class eligible for
// stale-if-error fallback (transport fault, UNAVAILABLE, DEADLINE_EXCEEDED).
type resolveError struct {
	transport bool
	err       error
}

func (e *resolveError) Error() string { return e.err.Error() }
func (e *resolveError) Unwrap() error { return e.err }

func classifyStatus(err error) error {
	code := status.Code(err)
	transport := code == codes.Unavailable || code == codes.DeadlineExceeded
	return &resolveError{transport: transport, err: err}
}

// ResolverConfig holds session-resolution cache tunables (issue #243).
// Pass to NewWithSession via WithResolverConfig; DefaultResolverConfig is used
// when omitted.
type ResolverConfig struct {
	Capacity     int           // L1 LRU capacity
	L1TTL        time.Duration // positive entry TTL (hard staleness/revocation cap)
	NegTTL       time.Duration // negative tombstone TTL for unknown tokens
	StaleIfError time.Duration // serve stale on transport error; 0 = off
}

// DefaultResolverConfig returns the #243 defaults: capacity 10000, L1 TTL 30s,
// neg TTL 2s, stale-if-error off.
func DefaultResolverConfig() ResolverConfig {
	return ResolverConfig{
		Capacity:     10000,
		L1TTL:        30 * time.Second,
		NegTTL:       2 * time.Second,
		StaleIfError: 0,
	}
}

// sessionFetcher is the backend fill for a cache miss: the actual point read.
// A nil session with a nil error means the token is unknown (not_found).
type sessionFetcher interface {
	fetch(ctx context.Context, tokenHash string) (*ResolvedSession, error)
}

// cacheEntry is one L1 slot. outcome == nil is a negative tombstone.
type cacheEntry struct {
	outcome      *ResolvedSession
	freshUntil   time.Time
	staleUntil   time.Time
	effectiveTTL time.Duration
}

// lruCache is a bounded LRU keyed by token hash (map + doubly linked list,
// O(1) get/put/evict). get promotes to most-recently-used.
type lruCache struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List
	items    map[string]*list.Element
}

type lruItem struct {
	key   string
	entry cacheEntry
}

func newLruCache(capacity int) *lruCache {
	return &lruCache{
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

func (c *lruCache) get(key string) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.capacity == 0 {
		return cacheEntry{}, false
	}
	el, ok := c.items[key]
	if !ok {
		return cacheEntry{}, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*lruItem).entry, true
}

func (c *lruCache) peek(key string) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return cacheEntry{}, false
	}
	return el.Value.(*lruItem).entry, true
}

func (c *lruCache) put(key string, entry cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.capacity == 0 {
		return
	}
	if el, ok := c.items[key]; ok {
		el.Value.(*lruItem).entry = entry
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&lruItem{key: key, entry: entry})
	c.items[key] = el
	for c.ll.Len() > c.capacity {
		oldest := c.ll.Back()
		if oldest == nil {
			break
		}
		c.ll.Remove(oldest)
		delete(c.items, oldest.Value.(*lruItem).key)
	}
}

func (c *lruCache) remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.Remove(el)
		delete(c.items, key)
	}
}

// cachedResolver is the cache-tiered resolver over any sessionFetcher.
type cachedResolver struct {
	fetcher sessionFetcher
	cache   *lruCache
	flight  singleflight.Group
	cfg     ResolverConfig
}

func newCachedResolver(fetcher sessionFetcher, cfg ResolverConfig) *cachedResolver {
	return &cachedResolver{
		fetcher: fetcher,
		cache:   newLruCache(cfg.Capacity),
		cfg:     cfg,
	}
}

// effectiveTTL applies downward-only jitter U(0.8, 1.0): L1TTL is a hard cap.
func (r *cachedResolver) effectiveTTL() time.Duration {
	jitter := 0.8 + 0.2*rand.Float64()
	return time.Duration(float64(r.cfg.L1TTL) * jitter)
}

// resolve returns (session, nil) on a hit/fill, (nil, nil) for a known-unknown
// token (tombstone), and (nil, err) on a backend/transport fault.
func (r *cachedResolver) resolve(hash string) (*ResolvedSession, error) {
	now := time.Now()

	// 1. L1 lookup.
	if entry, ok := r.cache.get(hash); ok {
		wallValid := entry.outcome == nil || entry.outcome.ExpiresAt.After(now)
		if !wallValid {
			// Positive entry past its session expiry: forced miss.
			r.cache.remove(hash)
		} else if now.Before(entry.freshUntil) {
			// Hit. Refresh-ahead for hot positive entries.
			if entry.outcome != nil {
				remaining := entry.freshUntil.Sub(now)
				if remaining.Seconds() < 0.10*entry.effectiveTTL.Seconds() {
					r.spawnRefresh(hash)
				}
			}
			return entry.outcome, nil
		}
		// else stale (past freshUntil): fall through to fill.
	}

	// 2. Miss (or stale): capture a stale candidate, then single-flight fill.
	stale := r.staleCandidate(hash, now)
	v, err, _ := r.flight.Do(hash, func() (interface{}, error) {
		return r.fill(hash)
	})
	if err != nil {
		var re *resolveError
		if errors.As(err, &re) && re.transport && stale != nil {
			log.Printf("session resolver: serving stale entry on transport error: %v", err)
			return stale, nil
		}
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	return v.(*ResolvedSession), nil
}

func (r *cachedResolver) fill(hash string) (*ResolvedSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()

	fetched, err := r.fetcher.fetch(ctx, hash)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var entry cacheEntry
	if fetched != nil {
		eff := r.effectiveTTL()
		wallRemaining := time.Until(fetched.ExpiresAt)
		if wallRemaining < 0 {
			wallRemaining = 0
		}
		ttl := eff
		if wallRemaining < ttl {
			ttl = wallRemaining
		}
		entry = cacheEntry{
			outcome:      fetched,
			freshUntil:   now.Add(ttl),
			staleUntil:   now.Add(ttl + r.cfg.StaleIfError),
			effectiveTTL: eff,
		}
	} else {
		entry = cacheEntry{
			outcome:      nil,
			freshUntil:   now.Add(r.cfg.NegTTL),
			staleUntil:   now.Add(r.cfg.NegTTL),
			effectiveTTL: r.cfg.NegTTL,
		}
	}
	r.cache.put(hash, entry)
	return fetched, nil
}

func (r *cachedResolver) staleCandidate(hash string, now time.Time) *ResolvedSession {
	if r.cfg.StaleIfError == 0 {
		return nil
	}
	entry, ok := r.cache.peek(hash)
	if !ok || entry.outcome == nil {
		return nil
	}
	if entry.outcome.ExpiresAt.After(now) && now.Before(entry.staleUntil) {
		return entry.outcome
	}
	return nil
}

func (r *cachedResolver) spawnRefresh(hash string) {
	go func() {
		_, _, _ = r.flight.Do(hash, func() (interface{}, error) {
			return r.fill(hash)
		})
	}()
}

func (r *cachedResolver) evict(hash string) {
	r.cache.remove(hash)
}

// newSessionResolver builds the cache-tiered resolver over an am.SessionService
// channel.
func newSessionResolver(sessionConn *grpc.ClientConn, cfg ResolverConfig) *cachedResolver {
	fetcher := &grpcFetcher{client: proto.NewSessionServiceClient(sessionConn)}
	return newCachedResolver(fetcher, cfg)
}

// grpcFetcher fills over am.SessionService (the relying-party fleet path).
type grpcFetcher struct {
	client proto.SessionServiceClient
}

func (f *grpcFetcher) fetch(ctx context.Context, tokenHash string) (*ResolvedSession, error) {
	resp, err := f.client.Resolve(ctx, &proto.ResolveRequest{TokenHash: tokenHash})
	if err != nil {
		return nil, classifyStatus(err)
	}
	s := resp.GetSession()
	if s == nil {
		// unknown / expired / revoked — deliberately indistinguishable.
		return nil, nil
	}
	return &ResolvedSession{
		Principal: s.GetPrincipal(),
		TenantId:  s.GetTenantId(),
		ExpiresAt: time.Unix(s.GetExpiresAtUnixSeconds(), 0),
	}, nil
}


