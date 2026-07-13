package nioclient

import (
	"testing"

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
