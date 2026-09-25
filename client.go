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

func (s Timestamp) wire() *string {
	if s == "" {
		return nil
	}
	ts := string(s)
	return &ts
}

// ListResult is the outcome of List: the evaluation snapshot zookie and the
// objects on which the subject holds the relation. Pass Ts to a subsequent
// CheckWithTimestamp / ListWithTimestamp / Read for a consistent snapshot.
type ListResult struct {
	Ts   Timestamp
	Objs []string
}

// ExpandResult is the outcome of Expand: the evaluation snapshot zookie, leaf
// user ids, unresolved usersets as (ns, obj, rel), and granted wildcards.
type ExpandResult struct {
	Ts        Timestamp
	UserIds   []UserId
	Usersets  []UserSet
	Wildcards []Wildcard
}

// ReadResult is the outcome of Read: the evaluation snapshot zookie and the
// raw stored tuples matching the filters. Rewrite rules are not applied — use
// Expand for the effective userset.
type ReadResult struct {
	Ts     Timestamp
	Tuples []Tuple
}

// Tuple is a relationship edge for Write (add or delete) and Read results.
// Subject is required. Expires, when non-nil, sets the tuple condition to that
// unix second (UTC).
type Tuple struct {
	Ns      Ns
	Obj     Obj
	Rel     Rel
	Subject Subject
	Expires *time.Time
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
// Build with FilterByObject or FilterBySubject.
type ReadFilter struct {
	set *proto.TupleSet
	err error
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

// FilterBySubject reverse-reads tuples in ns whose subject is sub
// (paper §2.4.3 UserSetSpec). rel nil means all relations.
func FilterBySubject(ns Ns, sub Subject, rel *Rel) ReadFilter {
	spec := &proto.TupleSet_UserSetSpec{}
	if err := setUserSetSpecUser(spec, sub); err != nil {
		return ReadFilter{err: err}
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

// checkAPI is the shared CheckService + NamespaceService surface. Embedded by
// Client and SessionClient so both expose the same RPC methods.
type checkAPI struct {
	grpcClient   proto.CheckServiceClient
	nsClient     proto.NamespaceServiceClient
	observeCheck func(ns Ns, obj Obj, rel Rel, sub Subject, duration time.Duration, ok bool, isError bool)
	observeList  func(ns Ns, rel Rel, sub Subject, duration time.Duration, isError bool)
}

func newCheckAPI(checkConn *grpc.ClientConn) *checkAPI {
	return &checkAPI{
		grpcClient: proto.NewCheckServiceClient(checkConn),
		nsClient:   proto.NewNamespaceServiceClient(checkConn),
	}
}

// Client is an RPC-only check client (Check/List/Write/…). It has no session
// resolution and does not implement Wrapper — pass it to Wrap is a compile error.
// For HTTP middleware, use SessionClient via NewWithSession.
type Client struct {
	*checkAPI
}

// SessionClient is check plus am.SessionService resolution for HTTP Wrap.
// It implements Wrapper. Always built with a non-nil session channel.
type SessionClient struct {
	*checkAPI
	prefix          string
	sessionResolver *cachedResolver
}

// Compile-time: only SessionClient satisfies Wrapper from this package.
var _ Wrapper = (*SessionClient)(nil)

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
	return &Client{checkAPI: newCheckAPI(checkConn)}
}

// NewWithSession creates a SessionClient for HTTP Wrap: check RPCs on checkConn
// and token→principal resolution on sessionConn (am.SessionService on nio-client).
// The two connections are always distinct endpoints. Pass SessionOptions for
// prefix and resolver cache config; defaults apply when omitted.
func NewWithSession(checkConn, sessionConn *grpc.ClientConn, opts ...SessionOption) *SessionClient {
	o := sessionOptions{cfg: DefaultResolverConfig()}
	for _, opt := range opts {
		opt(&o)
	}
	return &SessionClient{
		checkAPI:        newCheckAPI(checkConn),
		prefix:          o.prefix,
		sessionResolver: newSessionResolver(sessionConn, o.cfg),
	}
}

func (c *SessionClient) Prefix() string {
	return c.prefix
}

// ResolveToken hashes an opaque session token in-process (sha256, hex — the raw
// token never leaves the process) and resolves it to the principal UserId to
// pass to check. found=false with a nil error means the token is
// unknown/expired/revoked; the caller redirects to signin without any check RPC.
// Only SessionClient has this method — RPC-only *Client cannot call it.
func (c *SessionClient) ResolveToken(_ context.Context, token string) (userId UserId, found bool, err error) {
	session, err := c.sessionResolver.resolve(TokenHash(token))
	if err != nil {
		return 0, false, fmt.Errorf("resolve session: %w", err)
	}
	if session == nil {
		return 0, false, nil
	}
	return session.Principal, true, nil
}

// WithObserveCheck sets the observe function for checks on an RPC-only client.
func (c *Client) WithObserveCheck(f func(ns Ns, obj Obj, rel Rel, sub Subject, duration time.Duration, ok bool, isError bool)) *Client {
	c.observeCheck = f
	return c
}

// WithObserveList sets the observe function for lists on an RPC-only client.
func (c *Client) WithObserveList(f func(ns Ns, rel Rel, sub Subject, duration time.Duration, isError bool)) *Client {
	c.observeList = f
	return c
}

// WithObserveCheck sets the observe function for checks on a SessionClient.
func (c *SessionClient) WithObserveCheck(f func(ns Ns, obj Obj, rel Rel, sub Subject, duration time.Duration, ok bool, isError bool)) *SessionClient {
	c.observeCheck = f
	return c
}

// WithObserveList sets the observe function for lists on a SessionClient.
func (c *SessionClient) WithObserveList(f func(ns Ns, rel Rel, sub Subject, duration time.Duration, isError bool)) *SessionClient {
	c.observeList = f
	return c
}

// List lists the objects sub has rel to at any current snapshot.
// Prefer ListResult when you need the evaluation zookie for chaining.
func (c *checkAPI) List(ctx context.Context, ns Ns, rel Rel, sub Subject) ([]string, error) {
	res, err := c.ListWithTimestamp(ctx, ns, rel, sub, TimestampEmpty)
	if err != nil {
		return nil, err
	}
	return res.Objs, nil
}

// ListResult lists objects and returns the evaluation snapshot zookie.
func (c *checkAPI) ListResult(ctx context.Context, ns Ns, rel Rel, sub Subject) (ListResult, error) {
	return c.ListWithTimestamp(ctx, ns, rel, sub, TimestampEmpty)
}

// ListWithTimestamp lists objects evaluated at a snapshot at least as fresh as ts.
// The returned Ts is the snapshot the server actually used.
func (c *checkAPI) ListWithTimestamp(ctx context.Context, ns Ns, rel Rel, sub Subject, ts Timestamp) (ListResult, error) {
	req := &proto.ListRequest{
		Ns:  string(ns),
		Rel: string(rel),
		Ts:  ts.wire(),
	}
	if err := setListRequestUser(req, sub); err != nil {
		return ListResult{}, fmt.Errorf("list %s,%s: %w", ns, rel, err)
	}
	begin := time.Now().UnixMilli()
	list, err := c.grpcClient.List(ctx, req)
	elapsed := time.Now().UnixMilli() - begin
	if c.observeList != nil {
		c.observeList(ns, rel, sub, time.Duration(elapsed)*time.Millisecond, err != nil)
	}
	if err != nil {
		return ListResult{}, fmt.Errorf("list %s,%s,%v: %w", ns, rel, sub, err)
	}
	return ListResult{
		Ts:   Timestamp(list.GetTs()),
		Objs: list.GetObjs(),
	}, nil
}

// Check checks if sub has rel on an object. For a UserId subject it returns
// the principal the server evaluated; for a UserSet or Wildcard subject the
// returned UserId is zero.
func (c *checkAPI) Check(ctx context.Context, ns Ns, obj Obj, rel Rel, sub Subject) (principal UserId, ok bool, err error) {
	return c.CheckWithTimestamp(ctx, ns, obj, rel, sub, TimestampEmpty)
}

// CheckWithTimestamp is Check evaluated at a snapshot at least as fresh as ts.
// An empty ts leaves the snapshot to the server.
func (c *checkAPI) CheckWithTimestamp(ctx context.Context, ns Ns, obj Obj, rel Rel, sub Subject, ts Timestamp) (principal UserId, ok bool, err error) {
	if rel == Impossible {
		return 0, false, nil
	}
	req := &proto.CheckRequest{
		Ns:  string(ns),
		Obj: string(obj),
		Rel: string(rel),
		Ts:  ts.wire(),
	}
	if err := setCheckRequestUser(req, sub); err != nil {
		return 0, false, fmt.Errorf("check %s,%s,%s: %w", ns, obj, rel, err)
	}
	begin := time.Now().UnixMilli()
	res, err := c.grpcClient.Check(ctx, req)
	elapsed := time.Now().UnixMilli() - begin
	if c.observeCheck != nil {
		c.observeCheck(ns, obj, rel, sub, time.Duration(elapsed)*time.Millisecond, res.GetOk(), err != nil)
	}
	if err != nil {
		return 0, false, err
	}
	principal, err = checkPrincipal(sub, res)
	if err != nil {
		return 0, false, err
	}
	return principal, res.GetOk(), nil
}

func checkPrincipal(sub Subject, res *proto.CheckResponse) (UserId, error) {
	if _, isUser := sub.(UserId); !isUser {
		return 0, nil
	}
	p := res.GetPrincipal()
	if p != nil {
		return NewUserId(p.GetId())
	}
	if res.GetOk() {
		return 0, ErrEmptyPrincipal
	}
	return 0, nil
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

// AddOne adds sub on ⟨ns, obj, rel⟩.
// Returns the commit zookie for read-your-writes (pass to CheckWithTimestamp).
func (c *checkAPI) AddOne(ctx context.Context, ns Ns, obj Obj, rel Rel, sub Subject) (Timestamp, error) {
	return c.Write(ctx, []Tuple{{Ns: ns, Obj: obj, Rel: rel, Subject: sub}}, nil, nil)
}

// AddOneWithExpires adds sub on ⟨ns, obj, rel⟩ until expires (UTC).
// Returns the commit zookie for read-your-writes.
func (c *checkAPI) AddOneWithExpires(ctx context.Context, ns Ns, obj Obj, rel Rel, sub Subject, expires time.Time) (Timestamp, error) {
	exp := expires.UTC()
	return c.Write(ctx, []Tuple{{Ns: ns, Obj: obj, Rel: rel, Subject: sub, Expires: &exp}}, nil, nil)
}

// DeleteOne deletes sub from ⟨ns, obj, rel⟩.
// Returns the commit zookie for read-your-writes.
func (c *checkAPI) DeleteOne(ctx context.Context, ns Ns, obj Obj, rel Rel, sub Subject) (Timestamp, error) {
	return c.Write(ctx, nil, []Tuple{{Ns: ns, Obj: obj, Rel: rel, Subject: sub}}, nil)
}

// AddParent adds an inheritance relationship using the quasi-standard relation "parent".
// Returns the commit zookie for read-your-writes.
func (c *checkAPI) AddParent(ctx context.Context, ns Ns, obj Obj, parentNs Ns, parentObj Obj) (Timestamp, error) {
	return c.AddOne(ctx, ns, obj, RelParent, UserSet{Ns: parentNs, Obj: parentObj, Rel: RelUnspecified})
}

// Write commits add and del tuples atomically. precondition is an optional OCC
// zookie (WriteRequest.ts); pass nil for an unconditional write. Returns the
// commit zookie for read-your-writes / chaining subsequent reads.
func (c *checkAPI) Write(ctx context.Context, add, del []Tuple, precondition *Timestamp) (Timestamp, error) {
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
		req.Ts = precondition.wire()
	}

	res, err := c.grpcClient.Write(ctx, req)
	if err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return Timestamp(res.GetTs()), nil
}

// ContentChangeCheck authorizes a content modification against the freshest
// snapshot (never a client-supplied zookie). Returns the evaluation zookie to
// store with the new content version.
func (c *checkAPI) ContentChangeCheck(ctx context.Context, ns Ns, obj Obj, rel Rel, sub Subject) (ContentChangeCheckResult, error) {
	req := &proto.ContentChangeCheckRequest{
		Ns:  string(ns),
		Obj: string(obj),
		Rel: string(rel),
	}
	if err := setContentChangeCheckRequestUser(req, sub); err != nil {
		return ContentChangeCheckResult{}, fmt.Errorf("content_change_check %s,%s,%s: %w", ns, obj, rel, err)
	}
	res, err := c.grpcClient.ContentChangeCheck(ctx, req)
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
func (c *checkAPI) Watch(ctx context.Context, ns Ns, startTs Timestamp) (*WatchStream, error) {
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
func (c *checkAPI) Expand(ctx context.Context, ns Ns, obj Obj, rel Rel) (ExpandResult, error) {
	return c.ExpandWithTimestamp(ctx, ns, obj, rel, TimestampEmpty)
}

// ExpandWithTimestamp expands at a snapshot at least as fresh as ts.
func (c *checkAPI) ExpandWithTimestamp(ctx context.Context, ns Ns, obj Obj, rel Rel, ts Timestamp) (ExpandResult, error) {
	res, err := c.grpcClient.Expand(ctx, &proto.ExpandRequest{
		Ns:  string(ns),
		Obj: string(obj),
		Rel: string(rel),
		Ts:  ts.wire(),
	})
	if err != nil {
		return ExpandResult{}, fmt.Errorf("expand %s,%s,%s: %w", ns, obj, rel, err)
	}
	out, err := expandResultFromProto(res)
	if err != nil {
		return ExpandResult{}, fmt.Errorf("expand %s,%s,%s: %w", ns, obj, rel, err)
	}
	return out, nil
}

func expandResultFromProto(res *proto.ExpandResponse) (ExpandResult, error) {
	out := ExpandResult{
		Ts:        Timestamp(res.GetTs()),
		UserIds:   make([]UserId, 0, len(res.GetUserIds())),
		Usersets:  make([]UserSet, 0, len(res.GetUsersets())),
		Wildcards: make([]Wildcard, 0, len(res.GetWildcards())),
	}
	for _, n := range res.GetUserIds() {
		id, err := NewUserId(n)
		if err != nil {
			return ExpandResult{}, err
		}
		out.UserIds = append(out.UserIds, id)
	}
	for _, us := range res.GetUsersets() {
		out.Usersets = append(out.Usersets, userSetFromProto(us))
	}
	for _, w := range res.GetWildcards() {
		wc, err := wildcardFromProto(w)
		if err != nil {
			return ExpandResult{}, err
		}
		out.Wildcards = append(out.Wildcards, wc)
	}
	return out, nil
}

// ListNamespaces returns the namespace configs the check server loaded:
// per namespace the declared relations and the rewrite kind of each.
// Schema metadata only — no tuples.
func (c *checkAPI) ListNamespaces(ctx context.Context) ([]NamespaceMeta, error) {
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
func (c *checkAPI) GetAll(ctx context.Context, ns Ns, obj Obj) (ReadResult, error) {
	return c.Read(ctx, FilterByObject(ns, obj, nil))
}

// GetAllRel returns stored tuples on ⟨ns, obj, rel⟩.
func (c *checkAPI) GetAllRel(ctx context.Context, ns Ns, obj Obj, rel Rel) (ReadResult, error) {
	return c.Read(ctx, FilterByObject(ns, obj, &rel))
}

// ReadBySubject reverse-reads tuples in ns whose subject is sub.
// rel nil means all relations. Answered via the reverse index — no rewrites.
func (c *checkAPI) ReadBySubject(ctx context.Context, ns Ns, sub Subject, rel *Rel) (ReadResult, error) {
	return c.Read(ctx, FilterBySubject(ns, sub, rel))
}

// Read returns stored tuples matching filters at any current snapshot.
func (c *checkAPI) Read(ctx context.Context, filters ...ReadFilter) (ReadResult, error) {
	return c.ReadWithTimestamp(ctx, TimestampEmpty, filters...)
}

// ReadWithTimestamp returns stored tuples matching filters at a snapshot at
// least as fresh as ts. The returned Ts is the snapshot the server used.
func (c *checkAPI) ReadWithTimestamp(ctx context.Context, ts Timestamp, filters ...ReadFilter) (ReadResult, error) {
	if len(filters) == 0 {
		return ReadResult{}, errors.New("read: at least one filter required")
	}
	req := &proto.ReadRequest{
		Ts:        ts.wire(),
		TupleSets: make([]*proto.TupleSet, 0, len(filters)),
	}
	for i, f := range filters {
		if f.err != nil {
			return ReadResult{}, fmt.Errorf("read filter[%d]: %w", i, f.err)
		}
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
	if err := setTupleUser(pt, t.Subject); err != nil {
		return nil, fmt.Errorf("tuple %s:%s#%s: %w", t.Ns, t.Obj, t.Rel, err)
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

func tupleFromProto(pt *proto.Tuple) (Tuple, error) {
	if pt == nil {
		return Tuple{}, errors.New("nil tuple")
	}
	t := Tuple{
		Ns:  Ns(pt.GetNs()),
		Obj: Obj(pt.GetObj()),
		Rel: Rel(pt.GetRel()),
	}
	sub, err := tupleSubjectFromProto(pt)
	if err != nil {
		return Tuple{}, fmt.Errorf("tuple %s:%s#%s: %w", t.Ns, t.Obj, t.Rel, err)
	}
	t.Subject = sub
	if exp, ok := pt.GetCondition().(*proto.Tuple_Expires); ok {
		tm := time.Unix(exp.Expires, 0).UTC()
		t.Expires = &tm
	}
	return t, nil
}
