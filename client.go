package nioclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	proto "github.com/ecociel/nioclient-go/proto"
	"google.golang.org/grpc"
)

var (
	// ErrEmptyPrincipal is returned when a check is successful but the principal is empty.
	ErrEmptyPrincipal = errors.New("unexpected empty principal")
)

// Ns is a collection of objects.
type Ns string

// String returns the string representation of the namespace.
func (s Ns) String() string {
	return string(s)
}

// Built-in namespaces (nio domain / check bootstrap).
const (
	NsIam            = Ns("iam")
	NsServiceAccount = Ns("serviceaccount")
)

// Obj is an object.
type Obj string

// String returns the string representation of the object.
func (s Obj) String() string {

	return string(s)
}

// Built-in objects (nio domain).
// ObjRoot is the singleton object of the iam namespace: guards read iam:root#….
// ObjUnspecified is the "..." pointer keyword used as a parent-link object.
const (
	ObjRoot        = Obj("root")
	ObjUnspecified = Obj("...")
)

// Rel is a rel on an object.
type Rel string

// String returns the string representation of the rel.
func (s Rel) String() string {
	return string(s)
}

// Built-in relations (nio domain / check bootstrap).
// Roles (admin/editor/viewer) carry direct tuples; dotted names are computed
// permissions. The admin gate triple is NsIam + ObjRoot + RelIamGet|RelIamUpdate.
const (
	RelIs          = Rel("is")
	RelUnspecified = Rel("...")
	RelParent      = Rel("parent")

	RelAdmin  = Rel("admin")
	RelEditor = Rel("editor")
	RelViewer = Rel("viewer")

	RelIamGet    = Rel("iam.get")
	RelIamUpdate = Rel("iam.update")
	RelIamDelete = Rel("iam.delete")

	RelServiceAccountGet         = Rel("serviceaccount.get")
	RelServiceAccountCreate      = Rel("serviceaccount.create")
	RelServiceAccountUpdate      = Rel("serviceaccount.update")
	RelServiceAccountCreateToken = Rel("serviceaccount.createToken")
	RelServiceAccountKeyCreate   = Rel("serviceaccount.key.create")
	RelServiceAccountKeyGet      = Rel("serviceaccount.key.get")

	RelUserCreate = Rel("user.create")
)

// UserId is a user's ID.
type UserId string

// String returns the string representation of the user ID.
func (s UserId) String() string {
	return string(s)
}

// Public subject markers (nio domain UserId). Grantable like any other user id.
const (
	UserIdAllUsers           = UserId("allUsers")
	UserIdAuthenticatedUsers = UserId("authenticatedUsers")
)

// UserSet is a set of users.
type UserSet struct {
	Ns  Ns
	Obj Obj
	Rel Rel
}

// String returns the string representation of the user set.
func (s UserSet) String() string {
	return fmt.Sprintf("UserSet(Ns: %s, Obj: %s, Rel: %s)", s.Ns, s.Obj, s.Rel)
}

// Principal is a user or a group of users.
type Principal string

// Anonymous is the principal used for resources that do not require a session
const Anonymous = Principal("anonymous")

// String returns the string representation of the principal.
func (s Principal) String() string {
	return string(s)
}

// IsAnonymous returns true if the user is not known
func (s Principal) IsAnonymous() bool {
	return s == Anonymous
}

// Timestamp is an opaque packed zookie (standard Base64 of
// [epoch:u8][millis:u48 BE], 7 bytes). Store and echo only; do not invent.
type Timestamp string

// String returns the string representation of the timestamp.
func (s Timestamp) String() string {
	return string(s)
}

// TimestampEmpty is the packed empty zookie (epoch=1, millis=0). Wire value
// matches check's Timestamp::empty().pack() — Base64 of 01 00 00 00 00 00 00.
// Use when no fresher-than constraint is required (server picks a snapshot).
const TimestampEmpty = Timestamp("AQAAAAAAAA==")

// TimestampEpoch returns the empty evaluation zookie. Prefer TimestampEmpty.
func TimestampEpoch() Timestamp {
	return TimestampEmpty
}

// ListResult is the outcome of List: the evaluation snapshot zookie and the
// objects on which the subject holds the relation. Pass Ts to a subsequent
// CheckWithTimestamp / ListWithTimestamp / Read for a consistent snapshot.
type ListResult struct {
	Ts   Timestamp
	Objs []string
}

// ExpandResult is the outcome of Expand: the evaluation snapshot zookie, leaf
// user ids, and unresolved usersets as (ns, obj, rel).
type ExpandResult struct {
	Ts       Timestamp
	UserIds  []string
	Usersets []UserSet
}

// ReadResult is the outcome of Read: the evaluation snapshot zookie and the
// raw stored tuples matching the filters. Rewrite rules are not applied — use
// Expand for the effective userset.
type ReadResult struct {
	Ts     Timestamp
	Tuples []Tuple
}

// Tuple is a relationship edge for Write (add or delete) and Read results.
// Exactly one of UserId or UserSet must be set (UserSet non-nil wins if both).
type Tuple struct {
	Ns      Ns
	Obj     Obj
	Rel     Rel
	UserId  UserId   // set for a direct user subject; empty when using UserSet
	UserSet *UserSet // set for a userset subject
}

// ReadFilter is one TupleSet for the Read API (paper §2.4.2 / §2.4.3).
// Build with FilterByObject, FilterByUser, or FilterByUserSet.
type ReadFilter struct {
	set *proto.TupleSet
}

// FilterByObject reads stored tuples on ⟨ns, obj⟩. rel nil means all relations.
func FilterByObject(ns Ns, obj Obj, rel *Rel) ReadFilter {
	spec := &proto.TupleSet_ObjectSpec{Obj: string(obj)}
	if rel != nil {
		r := string(*rel)
		spec.Rel = &r
	}
	return ReadFilter{set: &proto.TupleSet{
		Ns:   string(ns),
		Spec: &proto.TupleSet_ObjectSpec_{ObjectSpec: spec},
	}}
}

// FilterByUser reverse-reads tuples in ns whose subject is userId
// (paper §2.4.3 UserSetSpec). rel nil means all relations.
func FilterByUser(ns Ns, userId UserId, rel *Rel) ReadFilter {
	spec := &proto.TupleSet_UserSetSpec{
		User: &proto.TupleSet_UserSetSpec_UserId{UserId: string(userId)},
	}
	if rel != nil {
		r := string(*rel)
		spec.Rel = &r
	}
	return ReadFilter{set: &proto.TupleSet{
		Ns:   string(ns),
		Spec: &proto.TupleSet_UsersetSpec{UsersetSpec: spec},
	}}
}

// FilterByUserSet reverse-reads tuples in ns whose subject is the userset.
// rel nil means all relations.
func FilterByUserSet(ns Ns, us UserSet, rel *Rel) ReadFilter {
	spec := &proto.TupleSet_UserSetSpec{
		User: &proto.TupleSet_UserSetSpec_UserSet{UserSet: &proto.UserSet{
			Ns:  string(us.Ns),
			Obj: string(us.Obj),
			Rel: string(us.Rel),
		}},
	}
	if rel != nil {
		r := string(*rel)
		spec.Rel = &r
	}
	return ReadFilter{set: &proto.TupleSet{
		Ns:   string(ns),
		Spec: &proto.TupleSet_UsersetSpec{UsersetSpec: spec},
	}}
}

// Client is a client for the check service.
type Client struct {
	// TODO prefix is a web concern only - should not be part of client
	prefix          string
	grpcClient      proto.CheckServiceClient
	sessionResolver *cachedResolver
	observeCheck    func(ns Ns, obj Obj, rel Rel, userId UserId, duration time.Duration, ok bool, isError bool)
	observeList     func(ns Ns, rel Rel, userId UserId, duration time.Duration, isError bool)
}

// New creates a new client for direct check/list/write RPCs. To serve web
// requests through Wrap, also configure the session channel with
// WithSessionConn so opaque session tokens can be resolved to a principal.
func New(conn *grpc.ClientConn) *Client {
	return &Client{
		prefix:     "",
		grpcClient: proto.NewCheckServiceClient(conn),
	}
}

func NewWithPrefix(conn *grpc.ClientConn, prefix string) *Client {
	if prefix == "/" {
		prefix = ""
	}
	return &Client{
		prefix:     prefix,
		grpcClient: proto.NewCheckServiceClient(conn),
	}
}

func (c *Client) Prefix() string {
	return c.prefix
}

// WithSessionConn configures token -> principal resolution over am.SessionService
// (issue #243/#245). Required to serve web requests through Wrap: the middleware
// hashes the cookie token in-process (sha256, hex — the raw token never leaves
// the process) and resolves it to a principal UUID before any check RPC. The
// relying party supplies a connected channel (mTLS to NIO_SESSION_URI); see
// DialSessionFromEnv.
func (c *Client) WithSessionConn(sessionConn *grpc.ClientConn) *Client {
	c.sessionResolver = newSessionResolver(sessionConn)
	return c
}

// ResolveToken hashes an opaque session token in-process (sha256, hex — the raw
// token never leaves the process) and resolves it to the principal UserId to
// pass to check. found=false with a nil error means the token is
// unknown/expired/revoked; the caller redirects to signin without any check RPC.
// It errors if no session channel was configured (see WithSessionConn) — there
// is no raw-token fallback.
func (c *Client) ResolveToken(_ context.Context, token string) (userId UserId, found bool, err error) {
	if c.sessionResolver == nil {
		return "", false, errors.New("nioclient: session resolver not configured (call WithSessionConn)")
	}
	session, err := c.sessionResolver.resolve(TokenHash(token))
	if err != nil {
		return "", false, fmt.Errorf("resolve session: %w", err)
	}
	if session == nil {
		return "", false, nil
	}
	return UserId(session.Principal), true, nil
}

// WithObserveCheck sets the observe function for checks.
// The observe function is called after each check.
// It can be used to collect metrics about the checks.
func (c *Client) WithObserveCheck(f func(ns Ns, obj Obj, rel Rel, userId UserId, duration time.Duration, ok bool, isError bool)) *Client {
	c.observeCheck = f
	return c
}

// WithObserveList sets the observe function for lists.
// The observe function is called after each list.
// It can be used to collect metrics about the lists.
func (c *Client) WithObserveList(f func(ns Ns, rel Rel, userId UserId, duration time.Duration, isError bool)) *Client {
	c.observeList = f
	return c
}

// List lists the objects a user has rel to at any current snapshot.
// Prefer ListResult when you need the evaluation zookie for chaining.
func (c *Client) List(ctx context.Context, ns Ns, rel Rel, userId UserId) ([]string, error) {
	res, err := c.ListWithTimestamp(ctx, ns, rel, userId, TimestampEmpty)
	if err != nil {
		return nil, err
	}
	return res.Objs, nil
}

// ListResult lists objects and returns the evaluation snapshot zookie.
func (c *Client) ListResult(ctx context.Context, ns Ns, rel Rel, userId UserId) (ListResult, error) {
	return c.ListWithTimestamp(ctx, ns, rel, userId, TimestampEmpty)
}

// ListWithTimestamp lists objects evaluated at a snapshot at least as fresh as ts.
// The returned Ts is the snapshot the server actually used.
func (c *Client) ListWithTimestamp(ctx context.Context, ns Ns, rel Rel, userId UserId, ts Timestamp) (ListResult, error) {
	begin := time.Now().UnixMilli()
	list, err := c.grpcClient.List(ctx, &proto.ListRequest{
		Ns:     string(ns),
		Rel:    string(rel),
		UserId: string(userId),
		Ts:     string(ts),
	})
	elapsed := time.Now().UnixMilli() - begin
	if c.observeList != nil {
		c.observeList(ns, rel, userId, time.Duration(elapsed)*time.Millisecond, err != nil)
	}
	if err != nil {
		return ListResult{}, fmt.Errorf("list %s,%s,%s: %w", ns, rel, userId, err)
	}
	return ListResult{
		Ts:   Timestamp(list.GetTs()),
		Objs: list.GetObjs(),
	}, nil
}

// Check checks if a user has a rel on an object.
// It returns the principal that granted the rel, whether the check was successful, and an error.
func (c *Client) Check(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (principal Principal, ok bool, err error) {
	return c.CheckWithTimestamp(ctx, ns, obj, rel, userId, TimestampEmpty)
}

// CheckWithTimestamp checks if a user has a rel on an object at a specific timestamp.
// It returns the principal that granted the rel, whether the check was successful, and an error.
func (c *Client) CheckWithTimestamp(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId, ts Timestamp) (principal Principal, ok bool, err error) {
	if rel == Impossible {
		return "", false, nil
	}
	begin := time.Now().UnixMilli()

	res, err := c.grpcClient.Check(ctx, &proto.CheckRequest{
		Ns:     string(ns),
		Obj:    string(obj),
		Rel:    string(rel),
		UserId: string(userId),
		Ts:     string(ts),
	})
	elapsed := time.Now().UnixMilli() - begin
	if c.observeCheck != nil {
		isOk := false
		if res != nil {
			isOk = res.Ok
		}
		c.observeCheck(ns, obj, rel, userId, time.Duration(elapsed)*time.Millisecond, isOk, err != nil)
	}
	if err != nil {
		return "", false, err
	}
	if !res.Ok {
		if res.Principal != nil {
			return Principal((*res.Principal).Id), false, nil
		} else {
			return "", false, nil
		}
	} else {
		if res.Principal != nil {
			return Principal((*res.Principal).Id), true, nil
		} else {
			return "", false, ErrEmptyPrincipal
		}
	}
}

// Keep for reference in case we need Basic auth again
//// NaiveBasicClient is a basic auth authenticator that holds a single
//// username and password.
//type NaiveBasicClient struct {
//	username string
//	password string
//}
//
//// NewNaiveBasicClient creates a new naive basic client.
//func NewNaiveBasicClient(username, password string) *NaiveBasicClient {
//	return &NaiveBasicClient{
//		username: username,
//		password: password,
//	}
//}
//
//// Authenticate authenticates a user with a username and password.
//// It returns whether the authentication was successful and an error.
//func (c *NaiveBasicClient) Authenticate(_ context.Context, username, password []byte) (bool, error) {
//	if string(username) != c.username {
//		return false, nil
//	}
//
//	return subtle.ConstantTimeCompare(password, []byte(c.password)) == 1, nil
//}

// AddOneUserId adds a user to an object with a specific relation.
// Returns the commit zookie for read-your-writes (pass to CheckWithTimestamp).
func (c *Client) AddOneUserId(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (Timestamp, error) {
	return c.Write(ctx, []Tuple{{
		Ns: ns, Obj: obj, Rel: rel, UserId: userId,
	}}, nil, nil)
}

// AddParent adds an inheritance relationship using the quasi-standard relation "parent".
// Returns the commit zookie for read-your-writes.
func (c *Client) AddParent(ctx context.Context, ns Ns, obj Obj, parentNs Ns, parentObj Obj) (Timestamp, error) {
	userSet := UserSet{
		Ns:  parentNs,
		Obj: parentObj,
		Rel: RelUnspecified,
	}
	return c.AddOneUserSet(ctx, ns, obj, RelParent, userSet)
}

// AddOneUserSet adds a userset subject on ⟨ns, obj, rel⟩.
// Returns the commit zookie for read-your-writes.
func (c *Client) AddOneUserSet(ctx context.Context, ns Ns, obj Obj, rel Rel, userSet UserSet) (Timestamp, error) {
	us := userSet
	return c.Write(ctx, []Tuple{{
		Ns: ns, Obj: obj, Rel: rel, UserSet: &us,
	}}, nil, nil)
}

// DeleteOneUserId deletes a user assignment on ⟨ns, obj, rel⟩.
// Returns the commit zookie for read-your-writes.
func (c *Client) DeleteOneUserId(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (Timestamp, error) {
	return c.Write(ctx, nil, []Tuple{{
		Ns: ns, Obj: obj, Rel: rel, UserId: userId,
	}}, nil)
}

// DeleteOneUserSet deletes a userset assignment on ⟨ns, obj, rel⟩.
// Returns the commit zookie for read-your-writes.
func (c *Client) DeleteOneUserSet(ctx context.Context, ns Ns, obj Obj, rel Rel, userSet UserSet) (Timestamp, error) {
	us := userSet
	return c.Write(ctx, nil, []Tuple{{
		Ns: ns, Obj: obj, Rel: rel, UserSet: &us,
	}}, nil)
}

// Write commits add and del tuples atomically. precondition is an optional OCC
// zookie (WriteRequest.ts); pass nil for an unconditional write. Returns the
// commit zookie for read-your-writes / chaining subsequent reads.
func (c *Client) Write(ctx context.Context, add, del []Tuple, precondition *Timestamp) (Timestamp, error) {
	req := &proto.WriteRequest{
		AddTuples: make([]*proto.Tuple, 0, len(add)),
		DelTuples: make([]*proto.Tuple, 0, len(del)),
	}
	for i := range add {
		pt, err := tupleToProto(&add[i])
		if err != nil {
			return "", fmt.Errorf("write add[%d]: %w", i, err)
		}
		req.AddTuples = append(req.AddTuples, pt)
	}
	for i := range del {
		pt, err := tupleToProto(&del[i])
		if err != nil {
			return "", fmt.Errorf("write del[%d]: %w", i, err)
		}
		req.DelTuples = append(req.DelTuples, pt)
	}
	if precondition != nil {
		ts := string(*precondition)
		req.Ts = &ts
	}

	res, err := c.grpcClient.Write(ctx, req)
	if err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return Timestamp(res.GetTs()), nil
}

// Expand returns the effective userset of ⟨ns, obj, rel⟩ (rewrite rules applied).
func (c *Client) Expand(ctx context.Context, ns Ns, obj Obj, rel Rel) (ExpandResult, error) {
	return c.ExpandWithTimestamp(ctx, ns, obj, rel, TimestampEmpty)
}

// ExpandWithTimestamp expands at a snapshot at least as fresh as ts.
func (c *Client) ExpandWithTimestamp(ctx context.Context, ns Ns, obj Obj, rel Rel, ts Timestamp) (ExpandResult, error) {
	res, err := c.grpcClient.Expand(ctx, &proto.ExpandRequest{
		Ns:  string(ns),
		Obj: string(obj),
		Rel: string(rel),
		Ts:  string(ts),
	})
	if err != nil {
		return ExpandResult{}, fmt.Errorf("expand %s,%s,%s: %w", ns, obj, rel, err)
	}
	usersets := make([]UserSet, 0, len(res.GetUsersets()))
	for _, us := range res.GetUsersets() {
		usersets = append(usersets, UserSet{
			Ns:  Ns(us.GetNs()),
			Obj: Obj(us.GetObj()),
			Rel: Rel(us.GetRel()),
		})
	}
	return ExpandResult{
		Ts:       Timestamp(res.GetTs()),
		UserIds:  res.GetUserIds(),
		Usersets: usersets,
	}, nil
}

// GetAll returns every stored tuple on ⟨ns, obj⟩ (all relations).
// Stored edges only — rewrites are not evaluated.
func (c *Client) GetAll(ctx context.Context, ns Ns, obj Obj) (ReadResult, error) {
	return c.Read(ctx, FilterByObject(ns, obj, nil))
}

// GetAllRel returns stored tuples on ⟨ns, obj, rel⟩.
func (c *Client) GetAllRel(ctx context.Context, ns Ns, obj Obj, rel Rel) (ReadResult, error) {
	return c.Read(ctx, FilterByObject(ns, obj, &rel))
}

// ReadByUser reverse-reads tuples in ns whose subject is userId.
// rel nil means all relations. Answered via the reverse index — no rewrites.
func (c *Client) ReadByUser(ctx context.Context, ns Ns, userId UserId, rel *Rel) (ReadResult, error) {
	return c.Read(ctx, FilterByUser(ns, userId, rel))
}

// ReadByUserSet reverse-reads tuples in ns whose subject is the userset.
// rel nil means all relations.
func (c *Client) ReadByUserSet(ctx context.Context, ns Ns, us UserSet, rel *Rel) (ReadResult, error) {
	return c.Read(ctx, FilterByUserSet(ns, us, rel))
}

// Read returns stored tuples matching filters at any current snapshot.
func (c *Client) Read(ctx context.Context, filters ...ReadFilter) (ReadResult, error) {
	return c.ReadWithTimestamp(ctx, TimestampEmpty, filters...)
}

// ReadWithTimestamp returns stored tuples matching filters at a snapshot at
// least as fresh as ts. The returned Ts is the snapshot the server used.
func (c *Client) ReadWithTimestamp(ctx context.Context, ts Timestamp, filters ...ReadFilter) (ReadResult, error) {
	if len(filters) == 0 {
		return ReadResult{}, errors.New("read: at least one filter required")
	}
	req := &proto.ReadRequest{
		TupleSets: make([]*proto.TupleSet, 0, len(filters)),
	}
	if ts != TimestampEmpty {
		s := string(ts)
		req.Ts = &s
	}
	for i, f := range filters {
		if f.set == nil {
			return ReadResult{}, fmt.Errorf("read filter[%d]: empty filter", i)
		}
		req.TupleSets = append(req.TupleSets, f.set)
	}

	res, err := c.grpcClient.Read(ctx, req)
	if err != nil {
		return ReadResult{}, fmt.Errorf("read: %w", err)
	}
	tuples := make([]Tuple, 0, len(res.GetTuples()))
	for i, pt := range res.GetTuples() {
		t, err := tupleFromProto(pt)
		if err != nil {
			return ReadResult{}, fmt.Errorf("read tuple[%d]: %w", i, err)
		}
		tuples = append(tuples, t)
	}
	return ReadResult{
		Ts:     Timestamp(res.GetTs()),
		Tuples: tuples,
	}, nil
}

func tupleToProto(t *Tuple) (*proto.Tuple, error) {
	if t == nil {
		return nil, errors.New("nil tuple")
	}
	pt := &proto.Tuple{
		Ns:  string(t.Ns),
		Obj: string(t.Obj),
		Rel: string(t.Rel),
	}
	if t.UserSet != nil {
		pt.User = &proto.Tuple_UserSet{UserSet: &proto.UserSet{
			Ns:  string(t.UserSet.Ns),
			Obj: string(t.UserSet.Obj),
			Rel: string(t.UserSet.Rel),
		}}
		return pt, nil
	}
	if t.UserId != "" {
		pt.User = &proto.Tuple_UserId{UserId: string(t.UserId)}
		return pt, nil
	}
	return nil, errors.New("tuple subject required (UserId or UserSet)")
}

// tupleFromProto maps a wire tuple to the client model. Missing user is a
// contract violation (NIO-003 / paper Read §2.4.2).
func tupleFromProto(pt *proto.Tuple) (Tuple, error) {
	if pt == nil {
		return Tuple{}, errors.New("nil tuple")
	}
	t := Tuple{
		Ns:  Ns(pt.GetNs()),
		Obj: Obj(pt.GetObj()),
		Rel: Rel(pt.GetRel()),
	}
	switch u := pt.GetUser().(type) {
	case nil:
		return Tuple{}, fmt.Errorf("tuple %s:%s#%s missing user field", t.Ns, t.Obj, t.Rel)
	case *proto.Tuple_UserId:
		t.UserId = UserId(u.UserId)
	case *proto.Tuple_UserSet:
		us := u.UserSet
		if us == nil {
			return Tuple{}, fmt.Errorf("tuple %s:%s#%s empty userset", t.Ns, t.Obj, t.Rel)
		}
		t.UserSet = &UserSet{
			Ns:  Ns(us.GetNs()),
			Obj: Obj(us.GetObj()),
			Rel: Rel(us.GetRel()),
		}
	default:
		return Tuple{}, fmt.Errorf("tuple %s:%s#%s unknown user type", t.Ns, t.Obj, t.Rel)
	}
	return t, nil
}
