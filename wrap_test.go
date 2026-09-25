package nioclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	resolvePrincipal UserId
	resolveErr       error
	checkCalls       int
	listCalls        int
	lastCheckSubject Subject
	checkSubjects    []Subject
	listSubjects     []Subject
}

func (w *resolvingWrapper) Prefix() string { return w.prefix }

func (w *resolvingWrapper) ResolveToken(_ context.Context, _ string) (UserId, bool, error) {
	if w.resolveErr != nil {
		return 0, false, w.resolveErr
	}
	if w.resolvePrincipal == 0 {
		return 0, false, nil
	}
	return w.resolvePrincipal, true, nil
}

func (w *resolvingWrapper) Check(_ context.Context, _ Ns, _ Obj, _ Rel, sub Subject) (UserId, bool, error) {
	w.checkCalls++
	w.lastCheckSubject = sub
	w.checkSubjects = append(w.checkSubjects, sub)
	id, _ := sub.(UserId)
	return id, true, nil
}

func (w *resolvingWrapper) CheckWithTimestamp(ctx context.Context, ns Ns, obj Obj, rel Rel, sub Subject, _ Timestamp) (UserId, bool, error) {
	return w.Check(ctx, ns, obj, rel, sub)
}

func (w *resolvingWrapper) List(_ context.Context, _ Ns, _ Rel, sub Subject) ([]string, error) {
	w.listCalls++
	w.listSubjects = append(w.listSubjects, sub)
	return []string{"granted"}, nil
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
	principal, authenticated := u.Principal()
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "ok:%s:%t", principal, authenticated)
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
	w := &resolvingWrapper{resolvePrincipal: 3}
	h := Wrap(w, extractTest, okHandler)

	rr := httptest.NewRecorder()
	h(rr, requestWithSession("raw-token"), nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if w.checkCalls != 1 {
		t.Fatalf("checkCalls = %d, want 1", w.checkCalls)
	}
	if w.lastCheckSubject != UserId(3) {
		t.Fatalf("check subject = %v, want the resolved principal 3", w.lastCheckSubject)
	}
	if body := rr.Body.String(); body != "ok:3:true" {
		t.Fatalf("body = %q, want ok:3:true", body)
	}
}

func TestWrapNotFoundRedirectsWithoutCheck(t *testing.T) {
	w := &resolvingWrapper{prefix: "/app", resolvePrincipal: 0}
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
	w := &resolvingWrapper{resolvePrincipal: 7}
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
	w := &resolvingWrapper{resolvePrincipal: 7}
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
	failing := func(_ context.Context, _ Ns, _ Obj, _ Rel, _ Subject) (UserId, bool, error) {
		calls++
		return 0, false, errors.New("boom")
	}
	m := newRequestMemo(failing, nil, nil)
	if _, _, err := m.check(context.Background(), "a", "b", "c", UserId(7)); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := m.check(context.Background(), "a", "b", "c", UserId(7)); err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Fatalf("errors must not be cached: calls = %d, want 2", calls)
	}
}

func TestRequestMemoCollapsesConcurrentMisses(t *testing.T) {
	var calls int32
	slow := func(_ context.Context, _ Ns, _ Obj, _ Rel, _ Subject) (UserId, bool, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond) // widen the in-flight window
		return 7, true, nil
	}
	m := newRequestMemo(slow, nil, nil)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = m.check(context.Background(), "a", "b", "c", UserId(7))
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("singleflight: underlying calls = %d, want 1", got)
	}
}

func TestRequestMemoListDedupesAndCopies(t *testing.T) {
	calls := 0
	lister := func(_ context.Context, _ Ns, _ Rel, _ Subject) ([]string, error) {
		calls++
		return []string{"x", "y"}, nil
	}
	m := newRequestMemo(nil, lister, nil)

	a, err := m.list(context.Background(), "n", "r", UserId(7))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := m.list(context.Background(), "n", "r", UserId(7)); err != nil {
		t.Fatalf("list: %v", err)
	}
	if calls != 1 {
		t.Fatalf("list must be memoized: calls = %d, want 1", calls)
	}

	// A caller mutating a returned slice must not corrupt the cache.
	a[0] = "MUTATED"
	c, _ := m.list(context.Background(), "n", "r", UserId(7))
	if c[0] != "x" {
		t.Fatalf("caller mutation leaked into the cache: got %q", c[0])
	}
}

func TestRequestMemoObserverReportsHitMiss(t *testing.T) {
	var events []string
	check := func(_ context.Context, _ Ns, _ Obj, _ Rel, _ Subject) (UserId, bool, error) {
		return 7, true, nil
	}
	m := newRequestMemo(check, nil, func(op string, hit bool) {
		events = append(events, op+":"+map[bool]string{true: "hit", false: "miss"}[hit])
	})

	_, _, _ = m.check(context.Background(), "a", "b", "c", UserId(7))
	_, _, _ = m.check(context.Background(), "a", "b", "c", UserId(7))

	if len(events) != 2 || events[0] != "check:miss" || events[1] != "check:hit" {
		t.Fatalf("observer events = %v, want [check:miss check:hit]", events)
	}
}

func anonymousProbeHandler(w http.ResponseWriter, _ *http.Request, _ httprouter.Params, _ Resource, u User) error {
	principal, authenticated := u.Principal()
	viewer, err := u.HasRel("viewer")
	if err != nil {
		return err
	}
	p9, err := u.HasRel("project", "p9", "viewer")
	if err != nil {
		return err
	}
	objs, err := u.List("project", "viewer")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "principal=%d authenticated=%t viewer=%t p9=%t list=%q nil=%t",
		principal, authenticated, viewer, p9, objs, objs == nil)
	return nil
}

func TestWrapPublicResourceNoCookieRunsAnonymous(t *testing.T) {
	w := &resolvingWrapper{}
	h := Wrap(w, extractPublicTest, anonymousProbeHandler)

	rr := httptest.NewRecorder()
	h(rr, requestWithSession(""), nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	want := `principal=0 authenticated=false viewer=false p9=false list=[] nil=false`
	if body := rr.Body.String(); body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
	if w.checkCalls != 0 || w.listCalls != 0 {
		t.Fatalf("check calls = %d, list calls = %d; want 0 and 0 for an anonymous caller", w.checkCalls, w.listCalls)
	}
}

func BenchmarkWrapMemoHasRel(b *testing.B) {
	w := &resolvingWrapper{resolvePrincipal: 7}
	h := Wrap(w, extractTest, memoProbeHandler, WithRequestMemo())
	req := requestWithSession("tok")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h(httptest.NewRecorder(), req, nil)
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
	w := &resolvingWrapper{resolvePrincipal: 7}
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

	wrapper := &resolvingWrapper{resolvePrincipal: 7}
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

type zeroPrincipalWrapper struct {
	resolvingWrapper
}

func (w *zeroPrincipalWrapper) Check(_ context.Context, _ Ns, _ Obj, _ Rel, _ Subject) (UserId, bool, error) {
	w.checkCalls++
	return 0, true, nil
}

func TestWrapRejectsGrantWithoutPrincipal(t *testing.T) {
	w := &zeroPrincipalWrapper{resolvingWrapper{resolvePrincipal: 7}}
	h := Wrap(w, extractTest, okHandler)

	rr := httptest.NewRecorder()
	h(rr, requestWithSession("tok"), nil)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%q", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); strings.HasPrefix(body, "ok:") {
		t.Fatalf("handler ran with body %q", body)
	}
}

func TestRequestMemoWaiterOnFailedFillReportsMiss(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	failing := func(_ context.Context, _ Ns, _ Obj, _ Rel, _ Subject) (UserId, bool, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return 0, false, errors.New("boom")
	}
	var mu sync.Mutex
	var events []string
	m := newRequestMemo(failing, nil, func(op string, hit bool) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, fmt.Sprintf("%s:%t", op, hit))
	})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = m.check(context.Background(), "a", "b", "c", UserId(7))
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("underlying calls = %d, want 1 (the waiter must share the leader's fill)", got)
	}
	if len(events) != 2 || events[0] != "check:false" || events[1] != "check:false" {
		t.Fatalf("observer events = %v, want [check:false check:false]", events)
	}
}

func TestWrapAuthenticatedHasRelAndListUseThePrincipal(t *testing.T) {
	w := &resolvingWrapper{resolvePrincipal: 7}
	h := Wrap(w, extractTest, func(rw http.ResponseWriter, _ *http.Request, _ httprouter.Params, _ Resource, u User) error {
		principal, authenticated := u.Principal()
		viewer, err := u.HasRel("viewer")
		if err != nil {
			return err
		}
		p9, err := u.HasRel("project", "p9", "viewer")
		if err != nil {
			return err
		}
		objs, err := u.List("project", "viewer")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(rw, "principal=%d authenticated=%t viewer=%t p9=%t list=%q", principal, authenticated, viewer, p9, objs)
		return nil
	})

	rr := httptest.NewRecorder()
	h(rr, requestWithSession("tok"), nil)

	want := `principal=7 authenticated=true viewer=true p9=true list=["granted"]`
	if rr.Code != http.StatusOK || rr.Body.String() != want {
		t.Fatalf("status=%d body=%q, want 200 %q", rr.Code, rr.Body.String(), want)
	}
	if !reflect.DeepEqual(w.checkSubjects, []Subject{UserId(7), UserId(7), UserId(7)}) {
		t.Fatalf("check subjects = %v, want the gate plus two HasRel calls for 7", w.checkSubjects)
	}
	if !reflect.DeepEqual(w.listSubjects, []Subject{UserId(7)}) {
		t.Fatalf("list subjects = %v, want one List call for 7", w.listSubjects)
	}
}
