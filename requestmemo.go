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

import (
	"context"
	"errors"
	"slices"
	"sync"
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

type checkFunc func(ctx context.Context, ns Ns, obj Obj, rel Rel, sub Subject) (UserId, bool, error)
type listFunc func(ctx context.Context, ns Ns, rel Rel, sub Subject) ([]string, error)

type checkMemoKey struct {
	ns  Ns
	obj Obj
	rel Rel
	sub Subject
}

type checkMemoVal struct {
	principal UserId
	ok        bool
}

type listMemoKey struct {
	ns  Ns
	rel Rel
	sub Subject
}

var errMemoFillAborted = errors.New("request memo: fill aborted")

type memoCell[V any] struct {
	done chan struct{}
	val  V
	err  error
}

type memoMap[K comparable, V any] struct {
	mu    sync.Mutex
	cells map[K]*memoCell[V]
}

func newMemoMap[K comparable, V any]() *memoMap[K, V] {
	return &memoMap[K, V]{cells: make(map[K]*memoCell[V])}
}

func (m *memoMap[K, V]) get(key K, fill func() (V, error)) (val V, hit bool, err error) {
	m.mu.Lock()
	cell, hit := m.cells[key]
	if !hit {
		cell = &memoCell[V]{done: make(chan struct{})}
		m.cells[key] = cell
	}
	m.mu.Unlock()

	if !hit {
		m.fill(key, cell, fill)
	}
	<-cell.done
	return cell.val, hit, cell.err
}

func (m *memoMap[K, V]) fill(key K, cell *memoCell[V], fill func() (V, error)) {
	cell.err = errMemoFillAborted
	defer func() {
		if cell.err != nil {
			m.mu.Lock()
			delete(m.cells, key)
			m.mu.Unlock()
		}
		close(cell.done)
	}()
	cell.val, cell.err = fill()
}

// requestMemo memoizes check and list decisions for the lifetime of one request.
// The timestamp is fixed per request (encoded in the wrapped check/list funcs),
// so it is not part of the keys.
type requestMemo struct {
	checks    *memoMap[checkMemoKey, checkMemoVal]
	lists     *memoMap[listMemoKey, []string]
	checkNext checkFunc
	listNext  listFunc
	observe   func(op string, hit bool)
}

func newRequestMemo(check checkFunc, list listFunc, observe func(op string, hit bool)) *requestMemo {
	return &requestMemo{
		checks:    newMemoMap[checkMemoKey, checkMemoVal](),
		lists:     newMemoMap[listMemoKey, []string](),
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

func (m *requestMemo) check(ctx context.Context, ns Ns, obj Obj, rel Rel, sub Subject) (UserId, bool, error) {
	key := checkMemoKey{ns: ns, obj: obj, rel: rel, sub: sub}
	val, hit, err := m.checks.get(key, func() (checkMemoVal, error) {
		principal, ok, err := m.checkNext(ctx, ns, obj, rel, sub)
		return checkMemoVal{principal: principal, ok: ok}, err
	})
	m.report("check", hit)
	if err != nil {
		return 0, false, err
	}
	return val.principal, val.ok, nil
}

func (m *requestMemo) list(ctx context.Context, ns Ns, rel Rel, sub Subject) ([]string, error) {
	key := listMemoKey{ns: ns, rel: rel, sub: sub}
	objs, hit, err := m.lists.get(key, func() ([]string, error) {
		objs, err := m.listNext(ctx, ns, rel, sub)
		return slices.Clone(objs), err
	})
	m.report("list", hit)
	if err != nil {
		return nil, err
	}
	return slices.Clone(objs), nil
}
