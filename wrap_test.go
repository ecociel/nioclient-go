package nioclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
)

// resolvingWrapper implements the Wrapper contract for the middleware tests.
type resolvingWrapper struct {
	prefix           string
	resolvePrincipal string // "" => not_found
	resolveErr       error
	checkCalls       int
	lastCheckUserId  UserId
}

func (w *resolvingWrapper) Prefix() string { return w.prefix }

func (w *resolvingWrapper) ResolveToken(_ context.Context, _ string) (UserId, bool, error) {
	if w.resolveErr != nil {
		return "", false, w.resolveErr
	}
	if w.resolvePrincipal == "" {
		return "", false, nil
	}
	return UserId(w.resolvePrincipal), true, nil
}

func (w *resolvingWrapper) Check(_ context.Context, _ Ns, _ Obj, _ Rel, userId UserId) (Principal, bool, error) {
	w.checkCalls++
	w.lastCheckUserId = userId
	return Principal(userId), true, nil
}

func (w *resolvingWrapper) CheckWithTimestamp(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId, _ Timestamp) (Principal, bool, error) {
	return w.Check(ctx, ns, obj, rel, userId)
}

func (w *resolvingWrapper) List(_ context.Context, _ Ns, _ Rel, _ UserId) ([]string, error) {
	return nil, nil
}

type testResource struct{}

func (testResource) Requires(_ string) (Ns, Obj, Rel) {
	return Ns("article"), Obj("1"), Rel("article.get")
}

type testPublicResource struct{}

func (testPublicResource) Requires(_ string) (Ns, Obj, Rel) {
	return Ns("article"), Obj("1"), Rel("article.get")
}
func (testPublicResource) publicResource() {}

func extractTest(_ http.ResponseWriter, _ *http.Request, _ httprouter.Params) (Resource, error) {
	return testResource{}, nil
}

func extractPublicTest(_ http.ResponseWriter, _ *http.Request, _ httprouter.Params) (Resource, error) {
	return testPublicResource{}, nil
}

func okHandler(w http.ResponseWriter, _ *http.Request, _ httprouter.Params, _ Resource, u User) error {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok:" + u.Principal()))
	return nil
}

func requestWithSession(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/articles/1", nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
	}
	return req
}

func TestWrapResolvedTokenChecksPrincipal(t *testing.T) {
	w := &resolvingWrapper{resolvePrincipal: "PRINCIPAL-UUID"}
	h := Wrap(w, extractTest, okHandler)

	rr := httptest.NewRecorder()
	h(rr, requestWithSession("raw-token"), nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if w.checkCalls != 1 {
		t.Fatalf("checkCalls = %d, want 1", w.checkCalls)
	}
	if w.lastCheckUserId != UserId("PRINCIPAL-UUID") {
		t.Fatalf("check subject = %q, want the resolved principal", w.lastCheckUserId)
	}
	if body := rr.Body.String(); !strings.Contains(body, "PRINCIPAL-UUID") {
		t.Fatalf("body = %q, want resolved principal", body)
	}
}

func TestWrapNotFoundRedirectsWithoutCheck(t *testing.T) {
	w := &resolvingWrapper{prefix: "/app", resolvePrincipal: ""} // not_found
	h := Wrap(w, extractTest, okHandler)

	rr := httptest.NewRecorder()
	h(rr, requestWithSession("raw-token"), nil)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.HasPrefix(loc, "/app/signin?back=") {
		t.Fatalf("Location = %q, want /app/signin redirect", loc)
	}
	if w.checkCalls != 0 {
		t.Fatalf("checkCalls = %d, want 0 on not_found", w.checkCalls)
	}
}

func TestWrapResolveErrorReturns500WithoutCheck(t *testing.T) {
	w := &resolvingWrapper{resolveErr: errors.New("backend down")}
	h := Wrap(w, extractTest, okHandler)

	rr := httptest.NewRecorder()
	h(rr, requestWithSession("raw-token"), nil)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	if w.checkCalls != 0 {
		t.Fatalf("checkCalls = %d, want 0 on resolve error", w.checkCalls)
	}
}

func TestSessionClientImplementsWrapper(t *testing.T) {
	// *SessionClient is the only package type that implements Wrapper.
	// *Client deliberately does not (no ResolveToken / Prefix) — Wrap(New(...))
	// is a compile error.
	var _ Wrapper = (*SessionClient)(nil)
}

// memoProbeHandler runs the gate rel again (memo hit) plus a second rel twice.
func memoProbeHandler(w http.ResponseWriter, _ *http.Request, _ httprouter.Params, _ Resource, u User) error {
	_, _ = u.HasRel("article.get")  // same as the route's gate rel -> memo hit
	_, _ = u.HasRel("article.edit") // new key -> 1 underlying check
	_, _ = u.HasRel("article.edit") // memo hit
	w.WriteHeader(http.StatusOK)
	return nil
}

func TestWrapRequestMemoDedupesChecks(t *testing.T) {
	w := &resolvingWrapper{resolvePrincipal: "P"}
	h := Wrap(w, extractTest, memoProbeHandler, WithRequestMemo())

	rr := httptest.NewRecorder()
	h(rr, requestWithSession("tok"), nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	// gate(article.get) + article.edit = 2; the repeats are served from the memo.
	if w.checkCalls != 2 {
		t.Fatalf("with memo: checkCalls = %d, want 2", w.checkCalls)
	}
}

func TestWrapWithoutMemoRepeatsChecks(t *testing.T) {
	w := &resolvingWrapper{resolvePrincipal: "P"}
	h := Wrap(w, extractTest, memoProbeHandler) // no WithRequestMemo

	rr := httptest.NewRecorder()
	h(rr, requestWithSession("tok"), nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	// gate(get) + get + edit + edit = 4: every check hits the wrapper.
	if w.checkCalls != 4 {
		t.Fatalf("without memo: checkCalls = %d, want 4", w.checkCalls)
	}
}

func TestCheckMemoDoesNotCacheErrors(t *testing.T) {
	calls := 0
	failing := func(_ context.Context, _ Ns, _ Obj, _ Rel, _ UserId) (Principal, bool, error) {
		calls++
		return "", false, errors.New("boom")
	}
	m := newRequestMemo(failing, nil, nil)
	if _, _, err := m.check(context.Background(), "a", "b", "c", "u"); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := m.check(context.Background(), "a", "b", "c", "u"); err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Fatalf("errors must not be cached: calls = %d, want 2", calls)
	}
}

func TestRequestMemoSingleflightCollapsesConcurrentMisses(t *testing.T) {
	var calls int32
	slow := func(_ context.Context, _ Ns, _ Obj, _ Rel, _ UserId) (Principal, bool, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond) // widen the in-flight window
		return "P", true, nil
	}
	m := newRequestMemo(slow, nil, nil)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = m.check(context.Background(), "a", "b", "c", "u")
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("singleflight: underlying calls = %d, want 1", got)
	}
}

func TestRequestMemoListDedupesAndCopies(t *testing.T) {
	calls := 0
	lister := func(_ context.Context, _ Ns, _ Rel, _ UserId) ([]string, error) {
		calls++
		return []string{"x", "y"}, nil
	}
	m := newRequestMemo(nil, lister, nil)

	a, err := m.list(context.Background(), "n", "r", "u")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := m.list(context.Background(), "n", "r", "u"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if calls != 1 {
		t.Fatalf("list must be memoized: calls = %d, want 1", calls)
	}

	// A caller mutating a returned slice must not corrupt the cache.
	a[0] = "MUTATED"
	c, _ := m.list(context.Background(), "n", "r", "u")
	if c[0] != "x" {
		t.Fatalf("caller mutation leaked into the cache: got %q", c[0])
	}
}

func TestRequestMemoObserverReportsHitMiss(t *testing.T) {
	var events []string
	check := func(_ context.Context, _ Ns, _ Obj, _ Rel, _ UserId) (Principal, bool, error) {
		return "P", true, nil
	}
	m := newRequestMemo(check, nil, func(op string, hit bool) {
		events = append(events, op+":"+map[bool]string{true: "hit", false: "miss"}[hit])
	})

	_, _, _ = m.check(context.Background(), "a", "b", "c", "u") // miss
	_, _, _ = m.check(context.Background(), "a", "b", "c", "u") // hit

	if len(events) != 2 || events[0] != "check:miss" || events[1] != "check:hit" {
		t.Fatalf("observer events = %v, want [check:miss check:hit]", events)
	}
}

func TestWrapPublicResourceNoCookieRunsAnonymous(t *testing.T) {
	w := &resolvingWrapper{}
	h := Wrap(w, extractPublicTest, okHandler)

	rr := httptest.NewRecorder()
	h(rr, requestWithSession(""), nil) // no session cookie

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if w.checkCalls != 0 {
		t.Fatalf("checkCalls = %d, want 0 for public resource", w.checkCalls)
	}
	if body := rr.Body.String(); !strings.Contains(body, string(Anonymous)) {
		t.Fatalf("body = %q, want anonymous principal", body)
	}
}

// problemErr satisfies Problemer so mapErrorAndRespond / a custom handler
// can return a non-500 status for known domain failures.
type problemErr struct {
	msg    string
	detail string
	status int
}

func (e problemErr) Error() string  { return e.msg }
func (e problemErr) Detail() string { return e.detail }
func (e problemErr) Status() int    { return e.status }

func errHandler(_ http.ResponseWriter, _ *http.Request, _ httprouter.Params, _ Resource, _ User) error {
	return fmt.Errorf("fetch task: %w", problemErr{msg: "not found", detail: "resource not found", status: http.StatusNotFound})
}

func TestWrapProblemerReturnsMappedStatus(t *testing.T) {
	// Default mapper must surface Problemer status codes (not 500).
	w := &resolvingWrapper{resolvePrincipal: "P"}
	h := Wrap(w, extractTest, errHandler)

	rr := httptest.NewRecorder()
	h(rr, requestWithSession("tok"), nil)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%q", rr.Code, rr.Body.String())
	}
}

func TestSetErrorHandlerIsInvokedByWrap(t *testing.T) {
	// Regression for the silent no-op: SetErrorHandler used to write a
	// variable that Wrap never read. Prove the custom mapper is on the wire.
	t.Cleanup(func() { SetErrorHandler(nil) })

	called := false
	SetErrorHandler(func(err error, w http.ResponseWriter, _ *http.Request) string {
		called = true
		http.Error(w, "custom:"+err.Error(), http.StatusTeapot)
		return ""
	})

	wrapper := &resolvingWrapper{resolvePrincipal: "P"}
	h := Wrap(wrapper, extractTest, func(http.ResponseWriter, *http.Request, httprouter.Params, Resource, User) error {
		return errors.New("boom")
	})

	rr := httptest.NewRecorder()
	h(rr, requestWithSession("tok"), nil)

	if !called {
		t.Fatal("custom error handler was never called")
	}
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418; body=%q", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "custom:boom") {
		t.Fatalf("body = %q, want custom:boom", body)
	}
}
