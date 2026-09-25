package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	nioclient "github.com/ecociel/nioclient-go"
)

func main() {
	ns := os.Args[1]
	obj := os.Args[2]
	rel := os.Args[3]
	sub, err := parseSubject(os.Args[4])
	if err != nil {
		log.Fatal(err)
	}

	conn, err := nioclient.DialCheckInsecure("localhost:50052")
	if err != nil {
		log.Fatalf("connect check-service: %v", err)
	}

	c := nioclient.New(conn)

	ts, err := c.AddOne(context.Background(), nioclient.Ns(ns), nioclient.Obj(obj), nioclient.Rel(rel), sub)
	if err != nil {
		log.Fatalf("add-one: %v", err)
	}
	fmt.Printf("committed at ts=%s\n", ts)
}

func parseSubject(s string) (nioclient.Subject, error) {
	switch s {
	case nioclient.AllUsers.String():
		return nioclient.AllUsers, nil
	case nioclient.AuthenticatedUsers.String():
		return nioclient.AuthenticatedUsers, nil
	}
	if !strings.Contains(s, "#") {
		return nioclient.ParseUserId(s)
	}
	ns, rest, okNs := strings.Cut(s, ":")
	obj, rel, okRel := strings.Cut(rest, "#")
	if !okNs || !okRel {
		return nil, errors.New("userset must be ns:obj#rel")
	}
	return nioclient.UserSet{Ns: nioclient.Ns(ns), Obj: nioclient.Obj(obj), Rel: nioclient.Rel(rel)}, nil
}
