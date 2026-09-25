package nioclient

import (
	"context"
	"fmt"
)

type User interface {
	// Principal returns the signed-in user's ID. false means anonymous.
	Principal() (UserId, bool)
	HasRel(args ...string) (bool, error)
	List(ns string, rel string) ([]string, error)
	IsAuthenticated() bool
}

type user struct {
	ns            Ns
	obj           Obj
	principal     UserId
	authenticated bool
	ctx           context.Context
	check         checkFunc
	list          listFunc
}

func (u *user) Principal() (UserId, bool) {
	return u.principal, u.authenticated
}

func (u *user) IsAuthenticated() bool {
	return u.authenticated
}

func (u *user) subject() Subject {
	if !u.authenticated {
		return AllUsers
	}
	return u.principal
}

func (u *user) HasRel(args ...string) (bool, error) {
	var ns Ns
	var obj Obj
	var rel Rel

	switch len(args) {
	case 1:
		ns = u.ns
		obj = u.obj
		rel = Rel(args[0])
	case 2:
		obj = Obj(args[0])
		rel = Rel(args[1])
	case 3:
		ns = Ns(args[0])
		obj = Obj(args[1])
		rel = Rel(args[2])
	default:
		panic("HasRel requires 1 or 3 arguments")
	}
	_, ok, err := u.check(u.ctx, ns, obj, rel, u.subject())
	if err != nil {
		return false, fmt.Errorf("user check: %s %s %s: %w", ns, obj, rel, err)
	}
	return ok, nil
}

func (u *user) List(ns string, rel string) ([]string, error) {
	objs, err := u.list(u.ctx, Ns(ns), Rel(rel), u.subject())
	if err != nil {
		return nil, fmt.Errorf("list: %s %s: %w", ns, rel, err)
	}
	return objs, nil
}
