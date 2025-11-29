package nioclient

import (
	"context"
	"fmt"
	"log"
)

type User interface {
	Principal() string
	HasRel(args ...string) (bool, error)
	List(ns string, rel string) ([]string, error)
}

type user struct {
	ns        Ns
	obj       Obj
	principal Principal
	ctx       context.Context
	check     func(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (principal Principal, ok bool, err error)
	list      func(ctx context.Context, ns Ns, rel Rel, userId UserId) ([]string, error)
}

func (u *user) Principal() string {
	return string(u.principal)
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
		break
	case 2:
		obj = Obj(args[0])
		rel = Rel(args[1])
		break
	case 3:
		ns = Ns(args[0])
		obj = Obj(args[1])
		rel = Rel(args[2])
		break
	default:
		panic("HasRel requires 1 or 3 arguments")
	}
	log.Printf("user check2: %s %s %s", ns, obj, rel)
	_, ok, err := u.check(u.ctx, ns, obj, rel, UserId(u.principal))
	if err != nil {
		return false, fmt.Errorf("user check: %s %s %s: %w", ns, obj, rel, err)
	}
	return ok, nil
}

func (u *user) List(ns string, rel string) ([]string, error) {
	log.Printf("list: %s %s", ns, rel)
	objs, err := u.list(u.ctx, Ns(ns), Rel(rel), UserId(u.principal))
	if err != nil {
		return nil, fmt.Errorf("list: %s %s: %w", ns, rel, err)
	}
	return objs, nil
}
