package nioclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	proto "github.com/ecociel/nioclient-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
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
// Expires, when non-nil, sets the tuple condition to that unix second (UTC).
type Tuple struct {
	Ns      Ns
	Obj     Obj
	Rel     Rel
	UserId  UserId     // set for a direct user subject; empty when using UserSet
	UserSet *UserSet   // set for a userset subject
	Expires *time.Time // optional absolute expiry; nil = no condition
}

// RelationMeta is schema metadata for one relation (name + rewrite kind).
// kind is one of this | computed | tuple_to | union.
type RelationMeta struct {
	Name string
	Kind string
}

// NamespaceMeta is schema metadata for one namespace loaded by check.
type NamespaceMeta struct {
	Name      string
	Relations []RelationMeta
}

// ContentChangeCheckResult is the outcome of ContentChangeCheck: whether the
// subject may modify content, and the evaluation snapshot zookie to store with
// the new content version.
type ContentChangeCheckResult struct {
	Ok bool
	Ts Timestamp
}

// WatchUpdate is one tuple change within an atomic write.
type WatchUpdate struct {
	Tuple   Tuple
	Deleted bool // true = tombstone (delete), false = add
}

// WatchEvent is one Watch stream message. Ts is the watermark: every change
// with commit ts <= Ts has been delivered. Empty Updates is a heartbeat
// (quiet watermark advance). A non-empty Updates batch is one atomic write
// committed at Ts — never split across messages — so any Ts is a safe resume
// point (exclusive) for a later Watch.
type WatchEvent struct {
	Ts      Timestamp
	Updates []WatchUpdate
}

// WatchStream is a server-streaming changelog tail for one namespace.
// Call Recv until io.EOF or a non-nil error; cancel the context passed to
// Watch to stop the stream.
type WatchStream struct {
	stream interface {
		Recv() (*proto.WatchResponse, error)
	}
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

// Client is a client for the check service, optionally with session resolution
// for HTTP Wrap (see New vs NewWithSession).
type Client struct {
	// TODO prefix is a web concern only - should not be part of client
	prefix          string
	grpcClient      proto.CheckServiceClient
	nsClient        proto.NamespaceServiceClient
	sessionResolver *cachedResolver
	observeCheck    func(ns Ns, obj Obj, rel Rel, userId UserId, duration time.Duration, ok bool, isError bool)
	observeList     func(ns Ns, rel Rel, userId UserId, duration time.Duration, isError bool)
}

// SessionOption configures NewWithSession (prefix, resolver cache tunables).
type SessionOption func(*sessionOptions)

type sessionOptions struct {
	prefix string
	cfg    ResolverConfig
}

// WithPrefix sets the URL prefix used by Wrap for sign-in redirects
// (e.g. "/app" → "/app/signin?back=…"). A lone "/" is treated as empty.
func WithPrefix(prefix string) SessionOption {
	return func(o *sessionOptions) {
		if prefix == "/" {
			prefix = ""
		}
		o.prefix = prefix
	}
}

// WithResolverConfig sets session L1 cache tunables. Omitted → DefaultResolverConfig.
func WithResolverConfig(cfg ResolverConfig) SessionOption {
	return func(o *sessionOptions) {
		o.cfg = cfg
	}
}

// New creates an RPC-only client on the check gRPC connection (CheckService +
// NamespaceService). Use for Check/List/Write/Read/Expand/Watch without cookie
// session resolution. For HTTP Wrap, use NewWithSession.
func New(checkConn *grpc.ClientConn) *Client {
	return &Client{
		grpcClient: proto.NewCheckServiceClient(checkConn),
		nsClient:   proto.NewNamespaceServiceClient(checkConn),
	}
}

// NewWithSession creates a client for HTTP Wrap: check RPCs on checkConn and
// token→principal resolution on sessionConn (am.SessionService on nio-client).
// The two connections are always distinct endpoints. Pass SessionOptions for
// prefix and resolver cache config; defaults apply when omitted.
func NewWithSession(checkConn, sessionConn *grpc.ClientConn, opts ...SessionOption) *Client {
	o := sessionOptions{cfg: DefaultResolverConfig()}
	for _, opt := range opts {
		opt(&o)
	}
	return &Client{
		prefix:          o.prefix,
		grpcClient:      proto.NewCheckServiceClient(checkConn),
		nsClient:        proto.NewNamespaceServiceClient(checkConn),
		sessionResolver: newSessionResolver(sessionConn, o.cfg),
	}
}

func (c *Client) Prefix() string {
	return c.prefix
}

// ResolveToken hashes an opaque session token in-process (sha256, hex — the raw
// token never leaves the process) and resolves it to the principal UserId to
// pass to check. found=false with a nil error means the token is
// unknown/expired/revoked; the caller redirects to signin without any check RPC.
// It errors if the client was built with New (RPC-only) rather than
// NewWithSession — there is no raw-token fallback.
func (c *Client) ResolveToken(_ context.Context, token string) (userId UserId, found bool, err error) {
	if c.sessionResolver == nil {
		return "", false, errors.New("nioclient: session resolver not configured (use NewWithSession)")
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

// AddOneUserIdWithExpires adds a user assignment that expires at expires (UTC).
// Returns the commit zookie for read-your-writes.
func (c *Client) AddOneUserIdWithExpires(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId, expires time.Time) (Timestamp, error) {
	exp := expires.UTC()
	return c.Write(ctx, []Tuple{{
		Ns: ns, Obj: obj, Rel: rel, UserId: userId, Expires: &exp,
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

// ContentChangeCheck authorizes a content modification against the freshest
// snapshot (never a client-supplied zookie). Returns the evaluation zookie to
// store with the new content version. userId may be a principal UUID or a
// userset string ns:obj#rel (same as Check).
func (c *Client) ContentChangeCheck(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (ContentChangeCheckResult, error) {
	res, err := c.grpcClient.ContentChangeCheck(ctx, &proto.ContentChangeCheckRequest{
		Ns:     string(ns),
		Obj:    string(obj),
		Rel:    string(rel),
		UserId: string(userId),
	})
	if err != nil {
		return ContentChangeCheckResult{}, fmt.Errorf("content_change_check %s,%s,%s: %w", ns, obj, rel, err)
	}
	return ContentChangeCheckResult{
		Ok: res.GetOk(),
		Ts: Timestamp(res.GetTs()),
	}, nil
}

// Watch starts a server-streaming tail of the changelog for ns (Zanzibar
// paper §2.4.6). Only changes committed after startTs are delivered,
// oldest-first, interleaved with heartbeats (empty Updates). Cancel ctx to
// stop. Resume later by passing any previously received event's Ts as startTs.
func (c *Client) Watch(ctx context.Context, ns Ns, startTs Timestamp) (*WatchStream, error) {
	stream, err := c.grpcClient.Watch(ctx, &proto.WatchRequest{
		Ns:      string(ns),
		StartTs: string(startTs),
	})
	if err != nil {
		return nil, fmt.Errorf("watch %s: %w", ns, err)
	}
	return &WatchStream{stream: stream}, nil
}

// Recv blocks until the next Watch event or an error. io.EOF means the stream
// ended cleanly (context cancelled or server closed).
func (s *WatchStream) Recv() (WatchEvent, error) {
	if s == nil || s.stream == nil {
		return WatchEvent{}, errors.New("watch: nil stream")
	}
	resp, err := s.stream.Recv()
	if err != nil {
		return WatchEvent{}, err
	}
	return watchEventFromProto(resp)
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

// ListNamespaces returns the namespace configs the check server loaded:
// per namespace the declared relations and the rewrite kind of each.
// Schema metadata only — no tuples.
func (c *Client) ListNamespaces(ctx context.Context) ([]NamespaceMeta, error) {
	res, err := c.nsClient.ListNamespaces(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	out := make([]NamespaceMeta, 0, len(res.GetNamespaces()))
	for _, ns := range res.GetNamespaces() {
		rels := make([]RelationMeta, 0, len(ns.GetRelations()))
		for _, r := range ns.GetRelations() {
			rels = append(rels, RelationMeta{
				Name: r.GetName(),
				Kind: r.GetKind(),
			})
		}
		out = append(out, NamespaceMeta{
			Name:      ns.GetName(),
			Relations: rels,
		})
	}
	return out, nil
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
	} else if t.UserId != "" {
		pt.User = &proto.Tuple_UserId{UserId: string(t.UserId)}
	} else {
		return nil, errors.New("tuple subject required (UserId or UserSet)")
	}
	if t.Expires != nil {
		pt.Condition = &proto.Tuple_Expires{Expires: t.Expires.UTC().Unix()}
	}
	return pt, nil
}

func watchEventFromProto(resp *proto.WatchResponse) (WatchEvent, error) {
	if resp == nil {
		return WatchEvent{}, errors.New("nil watch response")
	}
	ev := WatchEvent{
		Ts:      Timestamp(resp.GetTs()),
		Updates: make([]WatchUpdate, 0, len(resp.GetUpdates())),
	}
	for i, u := range resp.GetUpdates() {
		if u == nil {
			return WatchEvent{}, fmt.Errorf("watch update[%d]: nil", i)
		}
		t, err := tupleFromProto(u.GetTuple())
		if err != nil {
			return WatchEvent{}, fmt.Errorf("watch update[%d]: %w", i, err)
		}
		ev.Updates = append(ev.Updates, WatchUpdate{
			Tuple:   t,
			Deleted: u.GetDeleted(),
		})
	}
	return ev, nil
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
	if exp, ok := pt.GetCondition().(*proto.Tuple_Expires); ok {
		tm := time.Unix(exp.Expires, 0).UTC()
		t.Expires = &tm
	}
	return t, nil
}
