package nioclient

import (
	"strings"
	"testing"
	"time"

	proto "github.com/ecociel/nioclient-go/proto"
)

func TestTimestampEmptyIsPackedEmptyZookie(t *testing.T) {
	// check Timestamp::empty().pack() vector: Base64 of 01 00 00 00 00 00 00
	if TimestampEmpty != "AQAAAAAAAA==" {
		t.Fatalf("TimestampEmpty = %q, want AQAAAAAAAA==", TimestampEmpty)
	}
	if TimestampEpoch() != TimestampEmpty {
		t.Fatalf("TimestampEpoch() = %q, want TimestampEmpty", TimestampEpoch())
	}
}

// Domain constants must stay byte-identical to nio/domain (and check bootstrap).
func TestDomainConstantsMatchNio(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"NsIam", string(NsIam), "iam"},
		{"NsServiceAccount", string(NsServiceAccount), "serviceaccount"},
		{"ObjRoot", string(ObjRoot), "root"},
		{"ObjUnspecified", string(ObjUnspecified), "..."},
		{"RelIs", string(RelIs), "is"},
		{"RelUnspecified", string(RelUnspecified), "..."},
		{"RelParent", string(RelParent), "parent"},
		{"RelAdmin", string(RelAdmin), "admin"},
		{"RelEditor", string(RelEditor), "editor"},
		{"RelViewer", string(RelViewer), "viewer"},
		{"RelIamGet", string(RelIamGet), "iam.get"},
		{"RelIamUpdate", string(RelIamUpdate), "iam.update"},
		{"RelIamDelete", string(RelIamDelete), "iam.delete"},
		{"RelServiceAccountGet", string(RelServiceAccountGet), "serviceaccount.get"},
		{"RelServiceAccountCreate", string(RelServiceAccountCreate), "serviceaccount.create"},
		{"RelServiceAccountUpdate", string(RelServiceAccountUpdate), "serviceaccount.update"},
		{"RelServiceAccountCreateToken", string(RelServiceAccountCreateToken), "serviceaccount.createToken"},
		{"RelServiceAccountKeyCreate", string(RelServiceAccountKeyCreate), "serviceaccount.key.create"},
		{"RelServiceAccountKeyGet", string(RelServiceAccountKeyGet), "serviceaccount.key.get"},
		{"RelUserCreate", string(RelUserCreate), "user.create"},
		{"UserIdAllUsers", string(UserIdAllUsers), "allUsers"},
		{"UserIdAuthenticatedUsers", string(UserIdAuthenticatedUsers), "authenticatedUsers"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestTupleToProtoUserId(t *testing.T) {
	pt, err := tupleToProto(&Tuple{
		Ns: "doc", Obj: "1", Rel: "viewer", UserId: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pt.Ns != "doc" || pt.Obj != "1" || pt.Rel != "viewer" {
		t.Fatalf("fields: %+v", pt)
	}
	uid, ok := pt.User.(*proto.Tuple_UserId)
	if !ok || uid.UserId != "u1" {
		t.Fatalf("user: %#v", pt.User)
	}
}

func TestTupleFromProtoUserId(t *testing.T) {
	got, err := tupleFromProto(&proto.Tuple{
		Ns: "coll", Obj: "uk", Rel: "owner",
		User: &proto.Tuple_UserId{UserId: "user-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Ns != "coll" || got.Obj != "uk" || got.Rel != "owner" || got.UserId != "user-1" {
		t.Fatalf("got %+v", got)
	}
	if got.UserSet != nil {
		t.Fatalf("unexpected userset: %+v", got.UserSet)
	}
}

func TestTupleFromProtoUserSet(t *testing.T) {
	got, err := tupleFromProto(&proto.Tuple{
		Ns: "coll", Obj: "uk", Rel: "viewer",
		User: &proto.Tuple_UserSet{UserSet: &proto.UserSet{
			Ns: "grp", Obj: "eng", Rel: "member",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.UserSet == nil {
		t.Fatal("expected userset")
	}
	if got.UserSet.Ns != "grp" || got.UserSet.Obj != "eng" || got.UserSet.Rel != "member" {
		t.Fatalf("userset: %+v", got.UserSet)
	}
	if got.UserId != "" {
		t.Fatalf("unexpected userid %q", got.UserId)
	}
}

func TestTupleFromProtoMissingUser(t *testing.T) {
	_, err := tupleFromProto(&proto.Tuple{
		Ns: "coll", Obj: "uk", Rel: "owner",
	})
	if err == nil {
		t.Fatal("expected error for missing user")
	}
	if !containsAll(err.Error(), "missing user", "coll") {
		t.Fatalf("error should name field and tuple: %v", err)
	}
}

func TestFilterByObject(t *testing.T) {
	f := FilterByObject("doc", "1", nil)
	os := f.set.GetObjectSpec()
	if os == nil {
		t.Fatalf("expected object_spec, got %#v", f.set.Spec)
	}
	if f.set.Ns != "doc" || os.Obj != "1" || os.Rel != nil {
		t.Fatalf("filter: ns=%q obj=%q rel=%v", f.set.Ns, os.Obj, os.Rel)
	}

	rel := RelViewer
	f = FilterByObject("doc", "1", &rel)
	os = f.set.GetObjectSpec()
	if os == nil || os.GetRel() != "viewer" {
		t.Fatalf("expected rel viewer, got %#v", os)
	}
}

func TestFilterByUser(t *testing.T) {
	f := FilterByUser("doc", "u1", nil)
	us := f.set.GetUsersetSpec()
	if us == nil {
		t.Fatalf("expected userset_spec, got %#v", f.set.Spec)
	}
	if f.set.Ns != "doc" || us.GetUserId() != "u1" || us.Rel != nil {
		t.Fatalf("filter: ns=%q user=%q rel=%v", f.set.Ns, us.GetUserId(), us.Rel)
	}

	rel := RelEditor
	f = FilterByUser("doc", "u1", &rel)
	us = f.set.GetUsersetSpec()
	if us == nil || us.GetRel() != "editor" {
		t.Fatalf("expected rel editor, got %#v", us)
	}
}

func TestFilterByUserSet(t *testing.T) {
	f := FilterByUserSet("doc", UserSet{Ns: "grp", Obj: "eng", Rel: "member"}, nil)
	us := f.set.GetUsersetSpec()
	if us == nil {
		t.Fatalf("expected userset_spec, got %#v", f.set.Spec)
	}
	got := us.GetUserSet()
	if got == nil || got.Ns != "grp" || got.Obj != "eng" || got.Rel != "member" {
		t.Fatalf("userset: %+v", got)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestTupleToProtoUserSet(t *testing.T) {
	us := UserSet{Ns: "group", Obj: "eng", Rel: "member"}
	pt, err := tupleToProto(&Tuple{
		Ns: "doc", Obj: "1", Rel: "viewer", UserSet: &us,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := pt.User.(*proto.Tuple_UserSet)
	if !ok {
		t.Fatalf("user: %#v", pt.User)
	}
	if got.UserSet.Ns != "group" || got.UserSet.Obj != "eng" || got.UserSet.Rel != "member" {
		t.Fatalf("userset: %+v", got.UserSet)
	}
}

func TestTupleToProtoRequiresSubject(t *testing.T) {
	_, err := tupleToProto(&Tuple{Ns: "doc", Obj: "1", Rel: "viewer"})
	if err == nil {
		t.Fatal("expected error for missing subject")
	}
}

func TestTupleToProtoExpires(t *testing.T) {
	exp := time.Date(2030, 1, 15, 12, 0, 0, 0, time.UTC)
	pt, err := tupleToProto(&Tuple{
		Ns: "doc", Obj: "1", Rel: "viewer", UserId: "u1", Expires: &exp,
	})
	if err != nil {
		t.Fatal(err)
	}
	cond, ok := pt.Condition.(*proto.Tuple_Expires)
	if !ok {
		t.Fatalf("condition: %#v", pt.Condition)
	}
	if cond.Expires != exp.Unix() {
		t.Fatalf("expires = %d, want %d", cond.Expires, exp.Unix())
	}
}

func TestTupleFromProtoExpires(t *testing.T) {
	got, err := tupleFromProto(&proto.Tuple{
		Ns: "doc", Obj: "1", Rel: "viewer",
		User:      &proto.Tuple_UserId{UserId: "u1"},
		Condition: &proto.Tuple_Expires{Expires: 1894785600},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Expires == nil {
		t.Fatal("expected expires")
	}
	if got.Expires.Unix() != 1894785600 {
		t.Fatalf("expires = %d", got.Expires.Unix())
	}
	if got.Expires.Location() != time.UTC {
		t.Fatalf("location = %v, want UTC", got.Expires.Location())
	}
}

func TestTupleRoundTripExpires(t *testing.T) {
	exp := time.Unix(1894785600, 0).UTC()
	pt, err := tupleToProto(&Tuple{
		Ns: "doc", Obj: "1", Rel: "viewer", UserId: "u1", Expires: &exp,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := tupleFromProto(pt)
	if err != nil {
		t.Fatal(err)
	}
	if got.Expires == nil || !got.Expires.Equal(exp) {
		t.Fatalf("expires round-trip: got %v want %v", got.Expires, exp)
	}
}

func TestWatchEventFromProtoHeartbeat(t *testing.T) {
	ev, err := watchEventFromProto(&proto.WatchResponse{
		Ts:      "AQAAAAAAAA==",
		Updates: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Ts != TimestampEmpty {
		t.Fatalf("ts = %q", ev.Ts)
	}
	if len(ev.Updates) != 0 {
		t.Fatalf("heartbeat must have empty updates, got %d", len(ev.Updates))
	}
}

func TestWatchEventFromProtoAtomicWrite(t *testing.T) {
	ev, err := watchEventFromProto(&proto.WatchResponse{
		Ts: "commit-ts",
		Updates: []*proto.Update{
			{
				Tuple: &proto.Tuple{
					Ns: "doc", Obj: "1", Rel: "viewer",
					User: &proto.Tuple_UserId{UserId: "u1"},
				},
				Deleted: false,
			},
			{
				Tuple: &proto.Tuple{
					Ns: "doc", Obj: "1", Rel: "editor",
					User: &proto.Tuple_UserId{UserId: "u1"},
				},
				Deleted: true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Ts != "commit-ts" {
		t.Fatalf("ts = %q", ev.Ts)
	}
	if len(ev.Updates) != 2 {
		t.Fatalf("updates = %d", len(ev.Updates))
	}
	if ev.Updates[0].Deleted || ev.Updates[0].Tuple.UserId != "u1" {
		t.Fatalf("update[0]: %+v", ev.Updates[0])
	}
	if !ev.Updates[1].Deleted || ev.Updates[1].Tuple.Rel != "editor" {
		t.Fatalf("update[1]: %+v", ev.Updates[1])
	}
}

func TestWatchEventFromProtoMissingTupleUser(t *testing.T) {
	_, err := watchEventFromProto(&proto.WatchResponse{
		Ts: "t",
		Updates: []*proto.Update{
			{Tuple: &proto.Tuple{Ns: "doc", Obj: "1", Rel: "viewer"}},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing user")
	}
}

func TestWatchStreamNilRecv(t *testing.T) {
	var s *WatchStream
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected error on nil stream")
	}
	s = &WatchStream{}
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected error on empty stream")
	}
}

func TestTupleToProtoUserSetWinsOverUserId(t *testing.T) {
	us := UserSet{Ns: "g", Obj: "o", Rel: "r"}
	pt, err := tupleToProto(&Tuple{
		Ns: "doc", Obj: "1", Rel: "viewer", UserId: "u1", UserSet: &us,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pt.User.(*proto.Tuple_UserSet); !ok {
		t.Fatalf("expected userset, got %#v", pt.User)
	}
}
