package main

import (
	"context"
	"fmt"
	"log"
	"os"

	nioclient "github.com/ecociel/nioclient-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	ns := os.Args[1]
	rel := os.Args[2]
	userId := os.Args[3]

	hostport := "localhost:50052"

	conn, err := grpc.NewClient(hostport, grpc.
		WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("connect check-service at %q: %v", hostport, err)
	}

	c := nioclient.New(conn)

	res, err := c.ListResult(context.Background(), nioclient.Ns(ns), nioclient.Rel(rel), nioclient.UserId(userId))
	if err != nil {
		log.Fatalf("list: %v", err)
	}
	fmt.Printf("Result: %d objects at ts=%s\n", len(res.Objs), res.Ts)
	for _, obj := range res.Objs {
		fmt.Printf("%s\n", obj)
	}
}
