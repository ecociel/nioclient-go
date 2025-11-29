package nioclient

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	proto "github.com/ecociel/nioclient/proto"
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

// NsRoot is the root namespace.
const NsRoot = Ns("root")

// Obj is an object.
type Obj string

// String returns the string representation of the object.
func (s Obj) String() string {

	return string(s)
}

// ObjRoot is the root object.
const ObjRoot = Obj("root")

// Rel is a rel on an object.
type Rel string

// String returns the string representation of the rel.
func (s Rel) String() string {
	return string(s)
}

// RelUnspecified is the unspecified rel.
const RelUnspecified = Rel("...")
const RelParent = Rel("parent")

// UserId is a user's ID.
type UserId string

// String returns the string representation of the user ID.
func (s UserId) String() string {
	return string(s)
}

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

// String returns the string representation of the principal.
func (s Principal) String() string {
	return string(s)
}

// Timestamp is a timestamp.
type Timestamp string

// String returns the string representation of the timestamp.
func (s Timestamp) String() string {
	return string(s)
}

// TimestampEpoch returns the epoch timestamp.
func TimestampEpoch() Timestamp {
	return Timestamp("1:0000000000000")
}

// Client is a client for the check service.
type Client struct {
	grpcClient   proto.CheckServiceClient
	observeCheck func(ns Ns, obj Obj, rel Rel, userId UserId, duration time.Duration, ok bool, isError bool)
	observeList  func(ns Ns, rel Rel, userId UserId, duration time.Duration, isError bool)
}

// New creates a new client.
func New(conn *grpc.ClientConn) *Client {
	return &Client{
		grpcClient: proto.NewCheckServiceClient(conn),
	}
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

// List lists the objects a user has rel to.
// It returns a list of object IDs.
func (c *Client) List(ctx context.Context, ns Ns, rel Rel, userId UserId) ([]string, error) {
	begin := time.Now().UnixMilli()
	list, err := c.grpcClient.List(ctx, &proto.ListRequest{
		Ns:     string(ns),
		Rel:    string(rel),
		UserId: string(userId),
		Ts:     TimestampEpoch().String(),
	})
	elapsed := time.Now().UnixMilli() - begin
	if c.observeList != nil {
		c.observeList(ns, rel, userId, time.Duration(elapsed)*time.Millisecond, err != nil)
	}
	if err != nil {
		return nil, fmt.Errorf("list %s,%s,%s: %w", ns, rel, userId, err)
	}
	return list.Objs, nil
}

// Check checks if a user has a rel on an object.
// It returns the principal that granted the rel, whether the check was successful, and an error.
func (c *Client) Check(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (principal Principal, ok bool, err error) {
	return c.CheckWithTimestamp(ctx, ns, obj, rel, userId, Timestamp("1:0000000000000"))
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

// NaiveBasicClient is a basic auth authenticator that holds a single
// username and password.
type NaiveBasicClient struct {
	username string
	password string
}

// NewNaiveBasicClient creates a new naive basic client.
func NewNaiveBasicClient(username, password string) *NaiveBasicClient {
	return &NaiveBasicClient{
		username: username,
		password: password,
	}
}

// Authenticate authenticates a user with a username and password.
// It returns whether the authentication was successful and an error.
func (c *NaiveBasicClient) Authenticate(_ context.Context, username, password []byte) (bool, error) {
	if string(username) != c.username {
		return false, nil
	}

	return subtle.ConstantTimeCompare(password, []byte(c.password)) == 1, nil
}

//Ns:     string(ns),
//Rel:    string(rel),
//UserId: string(userId),

// AddOneUserId adds a user to an object with a specific relation.
func (c *Client) AddOneUserId(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) error {
	addTuple := proto.Tuple{
		Ns:   string(ns),
		Obj:  string(obj),
		Rel:  string(rel),
		User: &proto.Tuple_UserId{UserId: string(userId)},
	}

	_, err := c.grpcClient.Write(ctx, &proto.WriteRequest{
		AddTuples: []*proto.Tuple{&addTuple},
	})
	if err != nil {
		return fmt.Errorf("addOneUserId %s,%s,%s,%s: %w", ns, obj, rel, userId, err)
	}
	return nil
}

// AddParent adds an inheritance relationship using the quasi-stanard relation "parent.
func (c *Client) AddParent(ctx context.Context, ns Ns, obj Obj, parentNs Ns, parentObj Obj) error {
	userSet := UserSet{
		Ns:  parentNs,
		Obj: parentObj,
		Rel: RelUnspecified,
	}
	return c.AddOneUserSet(ctx, ns, obj, RelParent, userSet)
}

func (c *Client) AddOneUserSet(ctx context.Context, ns Ns, obj Obj, rel Rel, userSet UserSet) error {
	addTuple := proto.Tuple{
		Ns:  string(ns),
		Obj: string(obj),
		Rel: string(rel),
		User: &proto.Tuple_UserSet{UserSet: &proto.UserSet{
			Ns:  string(userSet.Ns),
			Obj: string(userSet.Obj),
			Rel: string(userSet.Rel),
		}},
	}

	_, err := c.grpcClient.Write(ctx, &proto.WriteRequest{
		AddTuples: []*proto.Tuple{&addTuple},
	})
	if err != nil {
		return fmt.Errorf("addOneUserSet %s,%s,%s,%s: %w", ns, obj, rel, userSet, err)
	}
	return nil
}
