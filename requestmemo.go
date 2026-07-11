package nioclient

// Request-scoped memoization of check and list decisions (option 1 of the
// client-cache design). Within the lifetime of a single HTTP request, identical
// check/list questions are answered from an in-request map instead of
// re-calling check over gRPC. A page that runs many HasRel/List calls for the
// same subject collapses to far fewer round-trips.
//
// This is SAFE with respect to staleness: a request is one logical instant, so
// asking the same authorization question twice within it must yield the same
// answer. The one caveat is read-after-write WITHIN a request — a handler that
// writes a tuple and then re-checks expecting to observe its own write. Such
// handlers must not enable the memo (it is opt-in per route via WithRequestMemo).
//
// Concurrent identical misses are collapsed with per-request singleflight, so a
// handler that fans checks out across goroutines still issues one RPC per key.

import (
	"context"
	"slices"
	"sync"

	"golang.org/x/sync/singleflight"
)

// wrapConfig holds per-route Wrap options.
type wrapConfig struct {
	requestMemo bool
	memoObserve func(op string, hit bool)
}

// WrapOption configures Wrap.
type WrapOption func(*wrapConfig)

// WithRequestMemo enables request-scoped memoization of check and list decisions
// for the routes it is applied to. Enable it on read handlers; do NOT enable it
// on a handler that writes a tuple and then re-checks expecting to see its own
// write.
func WithRequestMemo() WrapOption {
	return func(c *wrapConfig) { c.requestMemo = true }
}

// WithRequestMemoObserver enables request-scoped memoization (as WithRequestMemo)
// and reports each lookup to f. op is "check" or "list"; hit is true when the
// answer was served from the in-request cache.
func WithRequestMemoObserver(f func(op string, hit bool)) WrapOption {
	return func(c *wrapConfig) {
		c.requestMemo = true
		c.memoObserve = f
	}
}

type checkFunc func(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (Principal, bool, error)
type listFunc func(ctx context.Context, ns Ns, rel Rel, userId UserId) ([]string, error)

type checkMemoKey struct {
	ns, obj, rel, userId string
}

func (k checkMemoKey) string() string {
	return "c:" + k.ns + "\x00" + k.obj + "\x00" + k.rel + "\x00" + k.userId
}

type checkMemoVal struct {
	principal Principal
	ok        bool
}

type listMemoKey struct {
	ns, rel, userId string
}

func (k listMemoKey) string() string {
	return "l:" + k.ns + "\x00" + k.rel + "\x00" + k.userId
}

// requestMemo memoizes check and list decisions for the lifetime of one request.
// The timestamp is fixed per request (encoded in the wrapped check/list funcs),
// so it is not part of the keys.
type requestMemo struct {
	mu        sync.Mutex
	checks    map[checkMemoKey]checkMemoVal
	lists     map[listMemoKey][]string
	checkNext checkFunc
	listNext  listFunc
	flight    singleflight.Group
	observe   func(op string, hit bool)
}

func newRequestMemo(check checkFunc, list listFunc, observe func(op string, hit bool)) *requestMemo {
	return &requestMemo{
		checks:    make(map[checkMemoKey]checkMemoVal),
		lists:     make(map[listMemoKey][]string),
		checkNext: check,
		listNext:  list,
		observe:   observe,
	}
}

func (m *requestMemo) report(op string, hit bool) {
	if m.observe != nil {
		m.observe(op, hit)
	}
}

func (m *requestMemo) check(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (Principal, bool, error) {
	key := checkMemoKey{ns: string(ns), obj: string(obj), rel: string(rel), userId: string(userId)}

	m.mu.Lock()
	hit, ok := m.checks[key]
	m.mu.Unlock()
	if ok {
		m.report("check", true)
		return hit.principal, hit.ok, nil
	}
	m.report("check", false)

	v, err, _ := m.flight.Do(key.string(), func() (interface{}, error) {
		// Another goroutine may have filled the entry while we queued.
		m.mu.Lock()
		if hit, ok := m.checks[key]; ok {
			m.mu.Unlock()
			return hit, nil
		}
		m.mu.Unlock()

		principal, decision, err := m.checkNext(ctx, ns, obj, rel, userId)
		if err != nil {
			return checkMemoVal{}, err // never cache errors
		}
		val := checkMemoVal{principal: principal, ok: decision}
		m.mu.Lock()
		m.checks[key] = val
		m.mu.Unlock()
		return val, nil
	})
	if err != nil {
		return "", false, err
	}
	val := v.(checkMemoVal)
	return val.principal, val.ok, nil
}

func (m *requestMemo) list(ctx context.Context, ns Ns, rel Rel, userId UserId) ([]string, error) {
	key := listMemoKey{ns: string(ns), rel: string(rel), userId: string(userId)}

	m.mu.Lock()
	hit, ok := m.lists[key]
	m.mu.Unlock()
	if ok {
		m.report("list", true)
		return slices.Clone(hit), nil // callers must not mutate the cached slice
	}
	m.report("list", false)

	v, err, _ := m.flight.Do(key.string(), func() (interface{}, error) {
		m.mu.Lock()
		if hit, ok := m.lists[key]; ok {
			m.mu.Unlock()
			return hit, nil
		}
		m.mu.Unlock()

		objs, err := m.listNext(ctx, ns, rel, userId)
		if err != nil {
			return nil, err // never cache errors
		}
		stored := slices.Clone(objs)
		m.mu.Lock()
		m.lists[key] = stored
		m.mu.Unlock()
		return stored, nil
	})
	if err != nil {
		return nil, err
	}
	return slices.Clone(v.([]string)), nil
}
