package main

import (
	"context"
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
	user := os.Args[4]

	conn, err := nioclient.DialCheckInsecure("localhost:50052")
	if err != nil {
		log.Fatalf("connect check-service: %v", err)
	}

	c := nioclient.New(conn)

	var ts nioclient.Timestamp
	if strings.Contains(user, "#") {
		a := strings.SplitN(user, ":", 2)
		b := strings.SplitN(a[1], "#", 2)

		userSet := nioclient.UserSet{
			Ns:  nioclient.Ns(a[0]),
			Obj: nioclient.Obj(b[0]),
			Rel: nioclient.Rel(b[1]),
		}
		fmt.Printf("%v\n", userSet)
		ts, err = c.AddOneUserSet(context.Background(),
			nioclient.Ns(ns), nioclient.Obj(obj), nioclient.Rel(rel), userSet)
	} else {
		ts, err = c.AddOneUserId(context.Background(),
			nioclient.Ns(ns), nioclient.Obj(obj), nioclient.Rel(rel), nioclient.UserId(user))
	}
	if err != nil {
		log.Fatalf("add-one: %v", err)
	}
	fmt.Printf("committed at ts=%s\n", ts)
}
