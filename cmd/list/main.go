package main

import (
	"context"
	"fmt"
	nioclient "github.com/ecociel/nioclient-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"os"
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

	objs, err := c.List(context.Background(), nioclient.Ns(ns), nioclient.Rel(rel), nioclient.UserId(userId))
	if err != nil {
		log.Fatalf("list: %v", err)
	}
	fmt.Printf("Result: %d objects\n", len(objs))
	for _, obj := range objs {
		fmt.Printf("%s\n", obj)
	}

}
