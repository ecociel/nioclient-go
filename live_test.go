//go:build live

package nioclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	liveCheckTarget   = "localhost:50052"
	liveSessionTarget = "localhost:50053"
	liveTimeout       = 15 * time.Second
	visibilityTimeout = 30 * time.Second
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	conn, err := DialCheckInsecure(liveCheckTarget)
	if err != nil {
		t.Fatalf("dial check: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return New(conn)
}

func liveContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), liveTimeout)
	t.Cleanup(cancel)
	return ctx
}

func TestLiveCheckRejectsInvalidUserId(t *testing.T) {
	c := liveClient(t)
	for _, id := range []UserId{0, -1} {
		_, _, err := c.Check(liveContext(t), "project", "p42", "project.get", id)
		t.Logf("Check(UserId(%d)) error: %v", int64(id), err)
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("Check(UserId(%d)) code = %v, want InvalidArgument", int64(id), status.Code(err))
		}
	}
}

func TestLiveExpandReturnsEverySubjectKind(t *testing.T) {
	c := liveClient(t)
	ctx := liveContext(t)
	parent := UserSet{Ns: "project", Obj: "p42", Rel: RelUnspecified}

	ts, err := c.Write(ctx, []Tuple{
		{Ns: "project", Obj: "p11", Rel: "viewer", Subject: UserId(42)},
		{Ns: "project", Obj: "p11", Rel: "viewer", Subject: AllUsers},
		{Ns: "project", Obj: "p11", Rel: "viewer", Subject: parent},
	}, nil, nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := c.ExpandWithTimestamp(ctx, "project", "p11", "viewer", ts)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	t.Logf("expand project:p11#viewer = %+v", res)
	if !reflect.DeepEqual(res.UserIds, []UserId{42}) {
		t.Errorf("UserIds = %v, want [42]", res.UserIds)
	}
	if !reflect.DeepEqual(res.Wildcards, []Wildcard{AllUsers}) {
		t.Errorf("Wildcards = %v, want [allUsers]", res.Wildcards)
	}
	if !reflect.DeepEqual(res.Usersets, []UserSet{parent}) {
		t.Errorf("Usersets = %v, want [%v]", res.Usersets, parent)
	}
}

func TestLiveWatchAndReadBySubject(t *testing.T) {
	c := liveClient(t)
	ctx := liveContext(t)
	run := time.Now().UnixNano()
	obj := func(name string) Obj { return Obj(fmt.Sprintf("%s-%d", name, run)) }

	start, err := c.AddOne(ctx, "project", obj("watch-start"), "viewer", UserId(42))
	if err != nil {
		t.Fatalf("write start marker: %v", err)
	}
	stream, err := c.Watch(ctx, "project", start)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if _, err := c.AddOne(ctx, "project", obj("watch-user"), "viewer", UserId(42)); err != nil {
		t.Fatalf("write user tuple: %v", err)
	}
	last, err := c.AddOne(ctx, "project", obj("watch-all"), "viewer", AllUsers)
	if err != nil {
		t.Fatalf("write allUsers tuple: %v", err)
	}

	want := map[Obj]Subject{obj("watch-user"): UserId(42), obj("watch-all"): AllUsers}
	for len(want) > 0 {
		ev, err := stream.Recv()
		if err != nil {
			t.Fatalf("recv: %v (still waiting for %v)", err, want)
		}
		for _, u := range ev.Updates {
			t.Logf("watch event ts=%s tuple=%s:%s#%s@%v deleted=%t", ev.Ts, u.Tuple.Ns, u.Tuple.Obj, u.Tuple.Rel, u.Tuple.Subject, u.Deleted)
			if sub, ok := want[u.Tuple.Obj]; ok && u.Tuple.Subject == sub {
				delete(want, u.Tuple.Obj)
			}
		}
	}

	res, err := c.ReadWithTimestamp(ctx, last, FilterBySubject("project", UserId(42), nil))
	if err != nil {
		t.Fatalf("read by subject: %v", err)
	}
	t.Logf("read by subject 42: %d tuples", len(res.Tuples))
	var fromThisRun []Obj
	for _, tuple := range res.Tuples {
		if tuple.Subject != UserId(42) || tuple.Rel != "viewer" {
			t.Errorf("read returned %s:%s#%s@%v, want only viewer tuples for 42", tuple.Ns, tuple.Obj, tuple.Rel, tuple.Subject)
		}
		if strings.HasSuffix(string(tuple.Obj), fmt.Sprintf("-%d", run)) {
			fromThisRun = append(fromThisRun, tuple.Obj)
		}
	}
	slices.Sort(fromThisRun)
	wrote := []Obj{obj("watch-start"), obj("watch-user")}
	if !reflect.DeepEqual(fromThisRun, wrote) {
		t.Errorf("read by subject 42 returned this run's objects %v, want exactly %v", fromThisRun, wrote)
	}
}

func TestLiveAnonymousHasRelDeniesWithoutCheck(t *testing.T) {
	checkConn, err := DialCheckInsecure(liveCheckTarget)
	if err != nil {
		t.Fatalf("dial check: %v", err)
	}
	t.Cleanup(func() { _ = checkConn.Close() })
	sessionConn, err := DialSessionInsecure(liveSessionTarget)
	if err != nil {
		t.Fatalf("dial session: %v", err)
	}
	t.Cleanup(func() { _ = sessionConn.Close() })
	web := NewWithSession(checkConn, sessionConn)

	ctx := liveContext(t)
	if _, err := web.AddOne(ctx, "project", "p9", "viewer", AllUsers); err != nil {
		t.Fatalf("write project:p9#viewer@allUsers: %v", err)
	}
	if _, err := web.AddOne(ctx, "project", "p10", "viewer", AuthenticatedUsers); err != nil {
		t.Fatalf("write project:p10#viewer@authenticatedUsers: %v", err)
	}
	waitUntilAllowed(t, web.checkAPI, "p9", UserId(77))
	waitUntilAllowed(t, web.checkAPI, "p10", UserId(77))

	h := Wrap(web, extractPublicTest, func(w http.ResponseWriter, _ *http.Request, _ httprouter.Params, _ Resource, u User) error {
		var answers []string
		for _, obj := range []string{"p9", "p10", "p42"} {
			ok, err := u.HasRel("project", obj, "viewer")
			if err != nil {
				return err
			}
			answers = append(answers, fmt.Sprintf("%s=%t", obj, ok))
		}
		objs, err := u.List("project", "viewer")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "%s list=%q", strings.Join(answers, " "), objs)
		return nil
	})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, "/articles/1", nil), nil)

	t.Logf("anonymous public resource: status=%d body=%q", rr.Code, rr.Body.String())
	want := `p9=false p10=false p42=false list=[]`
	if rr.Code != http.StatusOK || rr.Body.String() != want {
		t.Fatalf("status=%d body=%q, want 200 %q", rr.Code, rr.Body.String(), want)
	}
}

func waitUntilAllowed(t *testing.T, c *checkAPI, obj Obj, sub Subject) {
	t.Helper()
	deadline := time.Now().Add(visibilityTimeout)
	for time.Now().Before(deadline) {
		_, ok, err := c.Check(liveContext(t), "project", obj, "viewer", sub)
		if err != nil {
			t.Fatalf("check project:%s#viewer@%v: %v", obj, sub, err)
		}
		if ok {
			t.Logf("project:%s#viewer visible to %v", obj, sub)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("project:%s#viewer not visible to %v within %s", obj, sub, visibilityTimeout)
}
