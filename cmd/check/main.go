package main

import (
	"context"
	"fmt"
	"log"
	"os"

	nioclient "github.com/ecociel/nioclient-go"
)

func main() {
	ns := os.Args[1]
	obj := os.Args[2]
	rel := os.Args[3]
	userId := os.Args[4]

	conn, err := nioclient.DialCheckInsecure("localhost:50052")
	if err != nil {
		log.Fatalf("connect check-service: %v", err)
	}

	c := nioclient.New(conn)

	principal, ok, err := c.CheckWithTimestamp(context.Background(), nioclient.Ns(ns), nioclient.Obj(obj), nioclient.Rel(rel), nioclient.UserId(userId), nioclient.TimestampEmpty)
	if err != nil {
		log.Fatalf("Error: %s", err.Error())
	}
	fmt.Printf("Result: %s %v", principal, ok)
}
