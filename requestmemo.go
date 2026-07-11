package nioclient

// Request-scoped memoization of check decisions (option 1 of the client-cache
// design). Within the lifetime of a single HTTP request, identical
// (ns, obj, rel, principal) checks are answered from an in-request map instead
// of re-calling check over gRPC. A page that runs many HasRel calls for the
// same subject/object collapses to far fewer round-trips.
//
// This is SAFE with respect to staleness: a request is one logical instant, so
// asking the same authorization question twice within it must yield the same
// answer. The one caveat is read-after-write WITHIN a request — a handler that
// writes a tuple and then re-checks expecting to observe its own write. Such
// handlers must not enable the memo (it is opt-in per route via WithRequestMemo).
//
// PROTOTYPE: covers Check only, no singleflight. See docs/request-memo-impl.md
// for the full scope (List memoization, singleflight, metrics).

import (
	"context"
	"sync"
)

// wrapConfig holds per-route Wrap options.
type wrapConfig struct {
	requestMemo bool
}

// WrapOption configures Wrap.
type WrapOption func(*wrapConfig)

// WithRequestMemo enables request-scoped memoization of check decisions for the
// routes it is applied to. Enable it on read handlers; do NOT enable it on a
// handler that writes a tuple and then re-checks expecting to see its own write.
func WithRequestMemo() WrapOption {
	return func(c *wrapConfig) { c.requestMemo = true }
}

type checkFunc func(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (Principal, bool, error)

type checkMemoKey struct {
	ns, obj, rel, userId string
}

type checkMemoVal struct {
	principal Principal
	ok        bool
}

// checkMemo memoizes check decisions for the lifetime of one request. The
// timestamp is fixed per request (encoded in the wrapped check func), so it is
// not part of the key.
type checkMemo struct {
	mu    sync.Mutex
	next  checkFunc
	cache map[checkMemoKey]checkMemoVal
}

func newCheckMemo(next checkFunc) *checkMemo {
	return &checkMemo{next: next, cache: make(map[checkMemoKey]checkMemoVal)}
}

func (m *checkMemo) check(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (Principal, bool, error) {
	key := checkMemoKey{ns: string(ns), obj: string(obj), rel: string(rel), userId: string(userId)}

	m.mu.Lock()
	hit, ok := m.cache[key]
	m.mu.Unlock()
	if ok {
		return hit.principal, hit.ok, nil
	}

	principal, decision, err := m.next(ctx, ns, obj, rel, userId)
	if err != nil {
		return principal, decision, err // never cache errors
	}

	m.mu.Lock()
	m.cache[key] = checkMemoVal{principal: principal, ok: decision}
	m.mu.Unlock()
	return principal, decision, nil
}
