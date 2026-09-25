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
	rel := os.Args[2]
	userId, err := nioclient.ParseUserId(os.Args[3])
	if err != nil {
		log.Fatal(err)
	}

	conn, err := nioclient.DialCheckInsecure("localhost:50052")
	if err != nil {
		log.Fatalf("connect check-service: %v", err)
	}

	c := nioclient.New(conn)

	res, err := c.ListResult(context.Background(), nioclient.Ns(ns), nioclient.Rel(rel), userId)
	if err != nil {
		log.Fatalf("list: %v", err)
	}
	fmt.Printf("Result: %d objects at ts=%s\n", len(res.Objs), res.Ts)
	for _, obj := range res.Objs {
		fmt.Printf("%s\n", obj)
	}
}
