package main

import (
	"fmt"
	"log"
	"net/http"

	nioclient "github.com/ecociel/nioclient-go"
	"github.com/julienschmidt/httprouter"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ArticleResource represents a single article, identified by its ID.
type ArticleResource struct {
	ID string
}

func (r *ArticleResource) Link() string {
	return fmt.Sprintf("/articles/%s", r.ID)
}

func (r *ArticleResource) Requires(method string) (nioclient.Ns, nioclient.Obj, nioclient.Rel) {
	var rel nioclient.Rel
	switch method {
	case http.MethodHead:
	case http.MethodGet:
		rel = "article.get"
	case http.MethodPost:
		rel = "article.update"
	default:
		rel = nioclient.Impossible
	}

	return nioclient.Ns("article"), nioclient.Obj(r.ID), rel
}

func ExtractArticleResource(w http.ResponseWriter, r *http.Request, p httprouter.Params) (nioclient.Resource, error) {
	id := p.ByName("id")
	if id == "" {
		panic("wrong router configuration")
	}
	return &ArticleResource{
		ID: id,
	}, nil
}

var RouteArticleResource = ArticleResource{
	ID: ":id",
}

func getArticle(w http.ResponseWriter, r *http.Request, p httprouter.Params, resource nioclient.Resource, user nioclient.User) error {
	articleResource := resource.(*ArticleResource)
	fmt.Fprintf(w, "Article id=%s", articleResource.ID)
	return nil
}

func main() {
	// 1. Connect to the NIO Authorization gRPC service
	checkHostPort := "localhost:50051"

	conn, err := grpc.NewClient(checkHostPort, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("connect check-service at %q: %v", checkHostPort, err)
	}

	nioClient := nioclient.New(conn)

	router := httprouter.New()

	router.GET(RouteArticleResource.Link(), nioclient.Wrap(nioClient, ExtractArticleResource, getArticle))

	log.Println("Starting server on port 8080...")
	if err := http.ListenAndServe("127.0.0.1:8080", router); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
