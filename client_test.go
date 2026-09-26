package nioclient

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	proto "github.com/ecociel/nioclient-go/proto"
	"google.golang.org/grpc"
	gproto "google.golang.org/protobuf/proto"
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
		{"AllUsers", AllUsers.String(), "allUsers"},
		{"AuthenticatedUsers", AuthenticatedUsers.String(), "authenticatedUsers"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
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

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestTupleToProtoRequiresSubject(t *testing.T) {
	_, err := tupleToProto(&Tuple{Ns: "doc", Obj: "1", Rel: "viewer"})
	if err == nil || err.Error() != "tuple doc:1#viewer: unsupported subject <nil>" {
		t.Fatalf("error = %v, want tuple doc:1#viewer: unsupported subject <nil>", err)
	}
}

func TestTupleToProtoExpires(t *testing.T) {
	exp := time.Date(2030, 1, 15, 12, 0, 0, 0, time.UTC)
	pt, err := tupleToProto(&Tuple{
		Ns: "doc", Obj: "1", Rel: "viewer", Subject: UserId(1), Expires: &exp,
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
		User:      &proto.Tuple_UserId{UserId: 1},
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
					User: &proto.Tuple_UserId{UserId: 1},
				},
				Deleted: false,
			},
			{
				Tuple: &proto.Tuple{
					Ns: "doc", Obj: "1", Rel: "editor",
					User: &proto.Tuple_UserId{UserId: 1},
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
	if ev.Updates[0].Deleted || ev.Updates[0].Tuple.Subject != UserId(1) {
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

type fakeCheckService struct {
	proto.CheckServiceClient
	checkReq  *proto.CheckRequest
	checkRes  *proto.CheckResponse
	listReq   *proto.ListRequest
	cccReq    *proto.ContentChangeCheckRequest
	expandRes *proto.ExpandResponse
}

func (f *fakeCheckService) List(_ context.Context, in *proto.ListRequest, _ ...grpc.CallOption) (*proto.ListResponse, error) {
	f.listReq = in
	return &proto.ListResponse{}, nil
}

func (f *fakeCheckService) ContentChangeCheck(_ context.Context, in *proto.ContentChangeCheckRequest, _ ...grpc.CallOption) (*proto.ContentChangeCheckResponse, error) {
	f.cccReq = in
	return &proto.ContentChangeCheckResponse{}, nil
}

func (f *fakeCheckService) Check(_ context.Context, in *proto.CheckRequest, _ ...grpc.CallOption) (*proto.CheckResponse, error) {
	f.checkReq = in
	return f.checkRes, nil
}

func (f *fakeCheckService) Expand(_ context.Context, _ *proto.ExpandRequest, _ ...grpc.CallOption) (*proto.ExpandResponse, error) {
	return f.expandRes, nil
}

func clientWith(f *fakeCheckService) *Client {
	return &Client{checkAPI: &checkAPI{grpcClient: f}}
}

func TestParseUserId(t *testing.T) {
	cases := []struct {
		in      string
		want    UserId
		wantErr string
	}{
		{"42", 42, ""},
		{"9223372036854775807", 9223372036854775807, ""},
		{"0", 0, "must be a decimal integer from 1 to 9223372036854775807"},
		{"-5", 0, "must be a decimal integer from 1 to 9223372036854775807"},
		{"01", 0, "must be a decimal integer from 1 to 9223372036854775807"},
		{"9223372036854775808", 0, "exceeds 9223372036854775807"},
		{"", 0, "must be a decimal integer from 1 to 9223372036854775807"},
		{"812eebc6-480b-4527-bed4-057e4d2fd1e3", 0, "must be a decimal integer from 1 to 9223372036854775807"},
	}
	for _, tc := range cases {
		got, err := ParseUserId(tc.in)
		if tc.wantErr == "" {
			if err != nil || got != tc.want {
				t.Errorf("ParseUserId(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("ParseUserId(%q) error = %v; want %q", tc.in, err, tc.wantErr)
		}
	}
}

func TestNewUserIdRejectsNonPositive(t *testing.T) {
	for _, n := range []int64{0, -1} {
		if _, err := NewUserId(n); err == nil {
			t.Errorf("NewUserId(%d) returned no error", n)
		}
	}
	if id, err := NewUserId(7); err != nil || id != 7 {
		t.Fatalf("NewUserId(7) = %d, %v", id, err)
	}
}

func TestTupleRoundTripsEverySubject(t *testing.T) {
	exp := time.Unix(1894785600, 0).UTC()
	cases := []struct {
		sub  Subject
		wire *proto.Tuple
	}{
		{UserId(42), &proto.Tuple{User: &proto.Tuple_UserId{UserId: 42}}},
		{UserSet{Ns: "grp", Obj: "eng", Rel: "member"}, &proto.Tuple{User: &proto.Tuple_UserSet{UserSet: &proto.UserSet{Ns: "grp", Obj: "eng", Rel: "member"}}}},
		{AllUsers, &proto.Tuple{User: &proto.Tuple_Wildcard{Wildcard: proto.Wildcard_ALL_USERS}}},
		{AuthenticatedUsers, &proto.Tuple{User: &proto.Tuple_Wildcard{Wildcard: proto.Wildcard_AUTHENTICATED_USERS}}},
	}
	for _, tc := range cases {
		tc.wire.Ns, tc.wire.Obj, tc.wire.Rel = "doc", "1", "viewer"
		tc.wire.Condition = &proto.Tuple_Expires{Expires: 1894785600}
		tuple := Tuple{Ns: "doc", Obj: "1", Rel: "viewer", Subject: tc.sub, Expires: &exp}

		pt, err := tupleToProto(&tuple)
		if err != nil {
			t.Fatalf("%v: to wire: %v", tc.sub, err)
		}
		if !gproto.Equal(pt, tc.wire) {
			t.Fatalf("%v: wire = %v, want %v", tc.sub, pt, tc.wire)
		}
		got, err := tupleFromProto(pt)
		if err != nil {
			t.Fatalf("%v: from wire: %v", tc.sub, err)
		}
		if !reflect.DeepEqual(got, tuple) {
			t.Fatalf("%v: round trip = %+v, want %+v", tc.sub, got, tuple)
		}
	}

	_, err := tupleFromProto(&proto.Tuple{
		Ns: "doc", Obj: "1", Rel: "viewer",
		User: &proto.Tuple_Wildcard{Wildcard: proto.Wildcard_WILDCARD_UNSPECIFIED},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown wildcard 0") {
		t.Fatalf("WILDCARD_UNSPECIFIED error = %v, want unknown wildcard 0", err)
	}
}

func TestTupleToProtoRejectsUnknownWildcard(t *testing.T) {
	_, err := tupleToProto(&Tuple{Ns: "doc", Obj: "1", Rel: "viewer", Subject: Wildcard(9)})
	if err == nil || !strings.Contains(err.Error(), "unknown wildcard 9") {
		t.Fatalf("error = %v, want unknown wildcard 9", err)
	}
}

func TestFilterBySubject(t *testing.T) {
	rel := RelEditor
	f := FilterBySubject("doc", AllUsers, &rel)
	want := &proto.TupleSet{
		Ns: "doc",
		Spec: &proto.TupleSet_UsersetSpec{UsersetSpec: &proto.TupleSet_UserSetSpec{
			User: &proto.TupleSet_UserSetSpec_Wildcard{Wildcard: proto.Wildcard_ALL_USERS},
			Rel:  gproto.String("editor"),
		}},
	}
	if f.err != nil || !gproto.Equal(f.set, want) {
		t.Fatalf("filter = %v, %v; want %v", f.set, f.err, want)
	}

	_, err := New(nil).Read(context.Background(), FilterBySubject("doc", nil, nil))
	if err == nil || !strings.Contains(err.Error(), "unsupported subject <nil>") {
		t.Fatalf("read with nil subject error = %v", err)
	}
}

func TestExpandMapsEverySubjectKind(t *testing.T) {
	f := &fakeCheckService{expandRes: &proto.ExpandResponse{
		Ts:        "AQAAAAAAAA==",
		UserIds:   []int64{42},
		Usersets:  []*proto.UserSet{{Ns: "project", Obj: "p42", Rel: "owner"}},
		Wildcards: []proto.Wildcard{proto.Wildcard_ALL_USERS},
	}}
	got, err := clientWith(f).Expand(context.Background(), "project", "p11", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	want := ExpandResult{
		Ts:        TimestampEmpty,
		UserIds:   []UserId{42},
		Usersets:  []UserSet{{Ns: "project", Obj: "p42", Rel: "owner"}},
		Wildcards: []Wildcard{AllUsers},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expand = %+v, want %+v", got, want)
	}

	f.expandRes = &proto.ExpandResponse{Wildcards: []proto.Wildcard{proto.Wildcard_WILDCARD_UNSPECIFIED}}
	if _, err := clientWith(f).Expand(context.Background(), "project", "p11", "viewer"); err == nil {
		t.Fatal("expand with WILDCARD_UNSPECIFIED returned no error")
	}
}

func TestCheckOmitsEmptyTs(t *testing.T) {
	f := &fakeCheckService{checkRes: &proto.CheckResponse{Ok: true, Principal: &proto.Principal{Id: 1}}}
	c := clientWith(f)

	if _, _, err := c.CheckWithTimestamp(context.Background(), "project", "p42", "project.get", UserId(1), TimestampEmpty); err != nil {
		t.Fatal(err)
	}
	if f.checkReq.Ts == nil || *f.checkReq.Ts != "AQAAAAAAAA==" {
		t.Fatalf("TimestampEmpty: Ts = %v, want AQAAAAAAAA==", f.checkReq.Ts)
	}

	if _, _, err := c.CheckWithTimestamp(context.Background(), "project", "p42", "project.get", UserId(1), ""); err != nil {
		t.Fatal(err)
	}
	if f.checkReq.Ts != nil {
		t.Fatalf(`"": Ts = %q, want nil`, *f.checkReq.Ts)
	}
}

func TestCheckPrincipalBySubject(t *testing.T) {
	cases := []struct {
		name    string
		sub     Subject
		res     *proto.CheckResponse
		want    UserId
		wantOk  bool
		wantErr error
	}{
		{"user granted", UserId(3), &proto.CheckResponse{Ok: true, Principal: &proto.Principal{Id: 3}}, 3, true, nil},
		{"user denied", UserId(3), &proto.CheckResponse{Ok: false, Principal: &proto.Principal{Id: 3}}, 3, false, nil},
		{"user granted without principal", UserId(3), &proto.CheckResponse{Ok: true}, 0, false, ErrEmptyPrincipal},
		{"userset granted", UserSet{Ns: "grp", Obj: "eng", Rel: "member"}, &proto.CheckResponse{Ok: true}, 0, true, nil},
		{"wildcard granted", AllUsers, &proto.CheckResponse{Ok: true}, 0, true, nil},
	}
	for _, tc := range cases {
		f := &fakeCheckService{checkRes: tc.res}
		got, ok, err := clientWith(f).Check(context.Background(), "project", "p42", "project.get", tc.sub)
		if got != tc.want || ok != tc.wantOk || err != tc.wantErr {
			t.Errorf("%s: Check = %d, %v, %v; want %d, %v, %v", tc.name, got, ok, err, tc.want, tc.wantOk, tc.wantErr)
		}
	}
}

func TestRequestsCarryUserSetAndWildcardSubjects(t *testing.T) {
	wireGrp := &proto.UserSet{Ns: "grp", Obj: "eng", Rel: "member"}
	empty := gproto.String("AQAAAAAAAA==")
	cases := []struct {
		sub   Subject
		check *proto.CheckRequest
		list  *proto.ListRequest
		ccc   *proto.ContentChangeCheckRequest
	}{
		{
			UserSet{Ns: "grp", Obj: "eng", Rel: "member"},
			&proto.CheckRequest{Ns: "doc", Obj: "1", Rel: "viewer", Ts: empty, User: &proto.CheckRequest_UserSet{UserSet: wireGrp}},
			&proto.ListRequest{Ns: "doc", Rel: "viewer", Ts: empty, User: &proto.ListRequest_UserSet{UserSet: wireGrp}},
			&proto.ContentChangeCheckRequest{Ns: "doc", Obj: "1", Rel: "viewer", User: &proto.ContentChangeCheckRequest_UserSet{UserSet: wireGrp}},
		},
		{
			AllUsers,
			&proto.CheckRequest{Ns: "doc", Obj: "1", Rel: "viewer", Ts: empty, User: &proto.CheckRequest_Wildcard{Wildcard: proto.Wildcard_ALL_USERS}},
			&proto.ListRequest{Ns: "doc", Rel: "viewer", Ts: empty, User: &proto.ListRequest_Wildcard{Wildcard: proto.Wildcard_ALL_USERS}},
			&proto.ContentChangeCheckRequest{Ns: "doc", Obj: "1", Rel: "viewer", User: &proto.ContentChangeCheckRequest_Wildcard{Wildcard: proto.Wildcard_ALL_USERS}},
		},
	}
	for _, tc := range cases {
		f := &fakeCheckService{checkRes: &proto.CheckResponse{Ok: true}}
		c := clientWith(f)
		ctx := context.Background()
		if _, _, err := c.Check(ctx, "doc", "1", "viewer", tc.sub); err != nil {
			t.Fatalf("%v: check: %v", tc.sub, err)
		}
		if _, err := c.List(ctx, "doc", "viewer", tc.sub); err != nil {
			t.Fatalf("%v: list: %v", tc.sub, err)
		}
		if _, err := c.ContentChangeCheck(ctx, "doc", "1", "viewer", tc.sub); err != nil {
			t.Fatalf("%v: content change check: %v", tc.sub, err)
		}
		if !gproto.Equal(f.checkReq, tc.check) {
			t.Errorf("%v: check request = %v, want %v", tc.sub, f.checkReq, tc.check)
		}
		if !gproto.Equal(f.listReq, tc.list) {
			t.Errorf("%v: list request = %v, want %v", tc.sub, f.listReq, tc.list)
		}
		if !gproto.Equal(f.cccReq, tc.ccc) {
			t.Errorf("%v: content change check request = %v, want %v", tc.sub, f.cccReq, tc.ccc)
		}
	}
}

func TestWriteOmitsEmptyPrecondition(t *testing.T) {
	f := &fakeWriteService{}
	c := &Client{checkAPI: &checkAPI{grpcClient: f}}
	tuples := []Tuple{{Ns: "doc", Obj: "1", Rel: "viewer", Subject: UserId(1)}}

	empty := Timestamp("")
	if _, err := c.Write(context.Background(), tuples, nil, &empty); err != nil {
		t.Fatal(err)
	}
	if f.req.Ts != nil {
		t.Fatalf(`precondition "": Ts = %q, want nil`, *f.req.Ts)
	}

	ts := TimestampEmpty
	if _, err := c.Write(context.Background(), tuples, nil, &ts); err != nil {
		t.Fatal(err)
	}
	if f.req.Ts == nil || *f.req.Ts != "AQAAAAAAAA==" {
		t.Fatalf("precondition TimestampEmpty: Ts = %v, want AQAAAAAAAA==", f.req.Ts)
	}
}

type fakeWriteService struct {
	proto.CheckServiceClient
	req *proto.WriteRequest
}

func (f *fakeWriteService) Write(_ context.Context, in *proto.WriteRequest, _ ...grpc.CallOption) (*proto.WriteResponse, error) {
	f.req = in
	return &proto.WriteResponse{Ts: "AQAAAAAAAA=="}, nil
}

type fakeReadService struct {
	proto.CheckServiceClient
	readReq  *proto.ReadRequest
	watchReq *proto.WatchRequest
}

func (f *fakeReadService) Read(_ context.Context, in *proto.ReadRequest, _ ...grpc.CallOption) (*proto.ReadResponse, error) {
	f.readReq = in
	return &proto.ReadResponse{}, nil
}

func (f *fakeReadService) Watch(_ context.Context, in *proto.WatchRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[proto.WatchResponse], error) {
	f.watchReq = in
	return nil, nil
}

func TestReadWithoutTimestampReadsLatest(t *testing.T) {
	f := &fakeReadService{}
	c := &Client{checkAPI: &checkAPI{grpcClient: f}}

	if _, err := c.GetAll(context.Background(), "doc", "1"); err != nil {
		t.Fatal(err)
	}
	if f.readReq.Ts != nil {
		t.Fatalf("GetAll: Ts = %q, want nil (latest snapshot)", *f.readReq.Ts)
	}

	if _, err := c.ReadWithTimestamp(context.Background(), TimestampEmpty, FilterByObject("doc", "1", nil)); err != nil {
		t.Fatal(err)
	}
	if f.readReq.Ts == nil || *f.readReq.Ts != "AQAAAAAAAA==" {
		t.Fatalf("ReadWithTimestamp(TimestampEmpty): Ts = %v, want AQAAAAAAAA==", f.readReq.Ts)
	}
}

func TestWatchRejectsEmptyStartTs(t *testing.T) {
	f := &fakeReadService{}
	c := &Client{checkAPI: &checkAPI{grpcClient: f}}

	_, err := c.Watch(context.Background(), "doc", "")
	if err == nil || err.Error() != "watch doc: start timestamp is required" {
		t.Fatalf("Watch(\"\"): err = %v, want start timestamp is required", err)
	}
	if f.watchReq != nil {
		t.Fatalf("Watch(\"\") sent a request: %v", f.watchReq)
	}

	if _, err := c.Watch(context.Background(), "doc", TimestampEmpty); err != nil {
		t.Fatal(err)
	}
	if f.watchReq.GetStartTs() != "AQAAAAAAAA==" {
		t.Fatalf("Watch(TimestampEmpty): StartTs = %q, want AQAAAAAAAA==", f.watchReq.GetStartTs())
	}
}

func TestWriteRejectsExpiresOnDelete(t *testing.T) {
	f := &fakeWriteService{}
	c := &Client{checkAPI: &checkAPI{grpcClient: f}}
	exp := time.Unix(1700000000, 0).UTC()
	del := []Tuple{{Ns: "doc", Obj: "1", Rel: "viewer", Subject: UserId(1), Expires: &exp}}

	_, err := c.Write(context.Background(), nil, del, nil)
	if err == nil || err.Error() != "write del[0]: tuple doc:1#viewer: a deleted tuple cannot carry Expires" {
		t.Fatalf("err = %v, want del[0] Expires rejection", err)
	}
	if f.req != nil {
		t.Fatalf("Write sent a request: %v", f.req)
	}
}
