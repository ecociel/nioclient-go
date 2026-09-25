package nioclient

import (
	"errors"
	"fmt"
	"strconv"

	proto "github.com/ecociel/nioclient-go/proto"
)

// Subject is who a tuple, check, or list is about. Only the value types
// UserId, UserSet, and Wildcard are accepted; a pointer to one of them also
// satisfies the interface but fails with an error when sent.
type Subject interface {
	isSubject()
}

// UserId is a user's ID, a positive int64. Build one from text with
// ParseUserId and from a number with NewUserId.
type UserId int64

func (UserId) isSubject() {}

// String returns the ID in plain decimal.
func (id UserId) String() string {
	return strconv.FormatInt(int64(id), 10)
}

// ParseUserId parses a decimal user ID from 1 to 9223372036854775807. It
// rejects zero, a sign, a leading zero, overflow, and any other text.
func ParseUserId(s string) (UserId, error) {
	if !isPositiveDecimal(s) {
		return 0, fmt.Errorf("user id %q: must be a decimal integer from 1 to 9223372036854775807", s)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("user id %q: exceeds 9223372036854775807", s)
	}
	return UserId(n), nil
}

func isPositiveDecimal(s string) bool {
	if s == "" || len(s) > 19 || s[0] == '0' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// NewUserId returns n as a UserId. It rejects n <= 0.
func NewUserId(n int64) (UserId, error) {
	if n <= 0 {
		return 0, fmt.Errorf("user id %d: must be a positive 64-bit integer", n)
	}
	return UserId(n), nil
}

// UserSet is the set of users that hold Rel on Ns:Obj.
type UserSet struct {
	Ns  Ns
	Obj Obj
	Rel Rel
}

func (UserSet) isSubject() {}

func (s UserSet) String() string {
	return fmt.Sprintf("UserSet(Ns: %s, Obj: %s, Rel: %s)", s.Ns, s.Obj, s.Rel)
}

func (s UserSet) toProto() *proto.UserSet {
	return &proto.UserSet{Ns: string(s.Ns), Obj: string(s.Obj), Rel: string(s.Rel)}
}

func userSetFromProto(us *proto.UserSet) UserSet {
	return UserSet{Ns: Ns(us.GetNs()), Obj: Obj(us.GetObj()), Rel: Rel(us.GetRel())}
}

// Wildcard is a public subject that stands for many users at once.
type Wildcard int32

// AllUsers and AuthenticatedUsers grant a relation to many users at once. nio
// f7569b9 treats both as a grant to every user ID (nio issue #316).
const (
	AllUsers           Wildcard = 1
	AuthenticatedUsers Wildcard = 2
)

func (Wildcard) isSubject() {}

// String returns allUsers or authenticatedUsers.
func (w Wildcard) String() string {
	switch w {
	case AllUsers:
		return "allUsers"
	case AuthenticatedUsers:
		return "authenticatedUsers"
	default:
		return fmt.Sprintf("Wildcard(%d)", int32(w))
	}
}

func (w Wildcard) toProto() (proto.Wildcard, error) {
	switch w {
	case AllUsers:
		return proto.Wildcard_ALL_USERS, nil
	case AuthenticatedUsers:
		return proto.Wildcard_AUTHENTICATED_USERS, nil
	default:
		return 0, fmt.Errorf("unknown wildcard %d", int32(w))
	}
}

func wildcardFromProto(w proto.Wildcard) (Wildcard, error) {
	switch w {
	case proto.Wildcard_ALL_USERS:
		return AllUsers, nil
	case proto.Wildcard_AUTHENTICATED_USERS:
		return AuthenticatedUsers, nil
	default:
		return 0, fmt.Errorf("unknown wildcard %d", int32(w))
	}
}

func unsupportedSubject(sub Subject) error {
	return fmt.Errorf("unsupported subject %T", sub)
}

func setCheckRequestUser(req *proto.CheckRequest, sub Subject) error {
	switch s := sub.(type) {
	case UserId:
		req.User = &proto.CheckRequest_UserId{UserId: int64(s)}
	case UserSet:
		req.User = &proto.CheckRequest_UserSet{UserSet: s.toProto()}
	case Wildcard:
		w, err := s.toProto()
		if err != nil {
			return err
		}
		req.User = &proto.CheckRequest_Wildcard{Wildcard: w}
	default:
		return unsupportedSubject(sub)
	}
	return nil
}

func setContentChangeCheckRequestUser(req *proto.ContentChangeCheckRequest, sub Subject) error {
	switch s := sub.(type) {
	case UserId:
		req.User = &proto.ContentChangeCheckRequest_UserId{UserId: int64(s)}
	case UserSet:
		req.User = &proto.ContentChangeCheckRequest_UserSet{UserSet: s.toProto()}
	case Wildcard:
		w, err := s.toProto()
		if err != nil {
			return err
		}
		req.User = &proto.ContentChangeCheckRequest_Wildcard{Wildcard: w}
	default:
		return unsupportedSubject(sub)
	}
	return nil
}

func setListRequestUser(req *proto.ListRequest, sub Subject) error {
	switch s := sub.(type) {
	case UserId:
		req.User = &proto.ListRequest_UserId{UserId: int64(s)}
	case UserSet:
		req.User = &proto.ListRequest_UserSet{UserSet: s.toProto()}
	case Wildcard:
		w, err := s.toProto()
		if err != nil {
			return err
		}
		req.User = &proto.ListRequest_Wildcard{Wildcard: w}
	default:
		return unsupportedSubject(sub)
	}
	return nil
}

func setTupleUser(pt *proto.Tuple, sub Subject) error {
	switch s := sub.(type) {
	case UserId:
		pt.User = &proto.Tuple_UserId{UserId: int64(s)}
	case UserSet:
		pt.User = &proto.Tuple_UserSet{UserSet: s.toProto()}
	case Wildcard:
		w, err := s.toProto()
		if err != nil {
			return err
		}
		pt.User = &proto.Tuple_Wildcard{Wildcard: w}
	default:
		return unsupportedSubject(sub)
	}
	return nil
}

func setUserSetSpecUser(spec *proto.TupleSet_UserSetSpec, sub Subject) error {
	switch s := sub.(type) {
	case UserId:
		spec.User = &proto.TupleSet_UserSetSpec_UserId{UserId: int64(s)}
	case UserSet:
		spec.User = &proto.TupleSet_UserSetSpec_UserSet{UserSet: s.toProto()}
	case Wildcard:
		w, err := s.toProto()
		if err != nil {
			return err
		}
		spec.User = &proto.TupleSet_UserSetSpec_Wildcard{Wildcard: w}
	default:
		return unsupportedSubject(sub)
	}
	return nil
}

func tupleSubjectFromProto(pt *proto.Tuple) (Subject, error) {
	switch u := pt.GetUser().(type) {
	case *proto.Tuple_UserId:
		return NewUserId(u.UserId)
	case *proto.Tuple_UserSet:
		if u.UserSet == nil {
			return nil, errors.New("empty userset")
		}
		return userSetFromProto(u.UserSet), nil
	case *proto.Tuple_Wildcard:
		return wildcardFromProto(u.Wildcard)
	case nil:
		return nil, errors.New("missing user field")
	default:
		return nil, fmt.Errorf("unknown user type %T", u)
	}
}
