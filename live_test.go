//go:build live

package nioclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
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

func uniqueObj(prefix string) Obj {
	return Obj(fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()))
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
	owners := UserSet{Ns: "project", Obj: "p42", Rel: "owner"}

	ts, err := c.Write(ctx, []Tuple{
		{Ns: "project", Obj: "p11", Rel: "viewer", Subject: UserId(42)},
		{Ns: "project", Obj: "p11", Rel: "viewer", Subject: AllUsers},
		{Ns: "project", Obj: "p11", Rel: "viewer", Subject: owners},
	}, nil, nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := c.ExpandWithTimestamp(ctx, "project", "p11", "viewer", ts)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	t.Logf("expand project:p11#viewer = %+v", res)
	if !slices.Contains(res.UserIds, UserId(42)) {
		t.Errorf("UserIds = %v, want 42", res.UserIds)
	}
	if !slices.Contains(res.Wildcards, AllUsers) {
		t.Errorf("Wildcards = %v, want AllUsers", res.Wildcards)
	}
	if !slices.Contains(res.Usersets, owners) {
		t.Errorf("Usersets = %v, want %v", res.Usersets, owners)
	}
}

func TestLiveWatchAndReadBySubject(t *testing.T) {
	c := liveClient(t)
	ctx := liveContext(t)
	objUser := uniqueObj("watch-user")
	objAll := uniqueObj("watch-all")

	start, err := c.AddOne(ctx, "project", uniqueObj("watch-start"), "viewer", UserId(42))
	if err != nil {
		t.Fatalf("write start marker: %v", err)
	}
	stream, err := c.Watch(ctx, "project", start)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if _, err := c.AddOne(ctx, "project", objUser, "viewer", UserId(42)); err != nil {
		t.Fatalf("write user tuple: %v", err)
	}
	last, err := c.AddOne(ctx, "project", objAll, "viewer", AllUsers)
	if err != nil {
		t.Fatalf("write allUsers tuple: %v", err)
	}

	want := map[Obj]Subject{objUser: UserId(42), objAll: AllUsers}
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
	found := false
	for _, tuple := range res.Tuples {
		if tuple.Subject != UserId(42) {
			t.Errorf("read returned tuple for %v, want only 42", tuple.Subject)
		}
		found = found || tuple.Obj == objUser
	}
	if !found {
		t.Errorf("read by subject 42 misses project:%s#viewer", objUser)
	}
}

func TestLiveAnonymousHasRelAsksAllUsers(t *testing.T) {
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
	waitUntilAllowed(t, web.checkAPI, "p9")

	h := Wrap(web, extractPublicTest, func(w http.ResponseWriter, _ *http.Request, _ httprouter.Params, _ Resource, u User) error {
		p9, err := u.HasRel("project", "p9", "viewer")
		if err != nil {
			return err
		}
		p42, err := u.HasRel("project", "p42", "viewer")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "p9=%t p42=%t", p9, p42)
		return nil
	})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, "/articles/1", nil), nil)

	t.Logf("anonymous public resource: status=%d body=%q", rr.Code, rr.Body.String())
	if rr.Code != http.StatusOK || rr.Body.String() != "p9=true p42=false" {
		t.Fatalf("status=%d body=%q, want 200 p9=true p42=false", rr.Code, rr.Body.String())
	}
}

func waitUntilAllowed(t *testing.T, c *checkAPI, obj Obj) {
	t.Helper()
	deadline := time.Now().Add(liveTimeout)
	for time.Now().Before(deadline) {
		_, ok, err := c.Check(liveContext(t), "project", obj, "viewer", AllUsers)
		if err != nil {
			t.Fatalf("check project:%s#viewer@allUsers: %v", obj, err)
		}
		if ok {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("project:%s#viewer@allUsers not visible within %s", obj, liveTimeout)
}
