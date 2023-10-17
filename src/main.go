package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"

	h "last_weekend_services/src/handlers"
	i "last_weekend_services/src/inits"
	middleware "last_weekend_services/src/middleware"

	"cloud.google.com/go/storage"
	"github.com/gorilla/mux"
	opensearch "github.com/opensearch-project/opensearch-go"
	"github.com/redis/go-redis/v9"
)

const (
	host     = "0.0.0.0"
	port     = 5432
	user     = "dmadonna"
	password = "1425"
	dbname   = "lw_db"
)

func main() {
	ctx := context.Background()

	// Postgres Initialization
	connString := fmt.Sprintf("user=%v password=%v host=%v port=%v dbname=%v", user, password, host, port, dbname)
	connPool, _ := i.CreatePostgresPool(connString, ctx)
	defer connPool.Pool.Close()

	// Redis Initialization
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})

	// GCP Storage Initialization
	gcpStorage, err := storage.NewClient(ctx)
	if err != nil {
		log.Panic(err)
	}

	// Opensearch Initialization
	openSearchClient, err := opensearch.NewClient(opensearch.Config{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Addresses: []string{"https://localhost:9200"},
		Username:  "admin", // For testing only. Don't store credentials in code.
		Password:  "admin"})
	if err != nil {
		fmt.Println("cannot initialize", err)
		os.Exit(1)
	}
	//log.Printf("OpenSearch Client Connected: %v", openSearchClient.Info)

	// ---------------------------------------------------------------------------------------
	//Only need the InitOpenSearch to run if we need to repopulate OpenSearch with the DB data
	//i.InitOpenSearch(ctx, connPool, openSearchClient)
	// ---------------------------------------------------------------------------------------

	//Server Starting String
	host := "0.0.0.0"
	port := "2525"
	serverString := fmt.Sprintf("%v:%v", host, port)

	r := mux.NewRouter()

	jwtMiddleware := middleware.EnsureValidToken()

	//Route Register
	r.HandleFunc("/", connPool.GETHandlerRoot)                                                                                     // Unprotected
	r.Handle("/ws", jwtMiddleware(h.WebSocketEndpointHandler(connPool, rdb, ctx)))                                                 // Protected
	r.Handle("/search", jwtMiddleware(h.SearchEndpointHandler(ctx, connPool, openSearchClient))).Methods("GET")                    // Protected
	r.Handle("/feed", jwtMiddleware(h.FeedEndpointHandler(ctx, connPool))).Methods("GET")                                          // Protected
	r.Handle("/image", jwtMiddleware(h.ContentEndpointHandler(ctx, *gcpStorage))).Methods("GET")                                   // Protected
	r.Handle("/upload", jwtMiddleware(h.ContentEndpointHandler(ctx, *gcpStorage))).Methods("GET")                                  // Protected
	r.Handle("/user", jwtMiddleware(h.UserEndpointHandler(connPool))).Methods("GET")                                               // Protected
	r.Handle("/user/album", jwtMiddleware(h.AlbumEndpointHandler(connPool, rdb, ctx))).Methods("GET", "POST")                      // Protected
	r.Handle("/user/album/image", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("GET", "POST")                // Protected
	r.Handle("/user/image", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("GET")                              // Protected
	r.Handle("/user/friend", jwtMiddleware(h.FriendEndpointHandler(ctx, connPool, rdb))).Methods("GET", "DELETE")                  // Protected
	r.Handle("/notifications", jwtMiddleware(h.NotificationsEndpointHandler(ctx, connPool, rdb))).Methods("GET")                   // Protected
	r.Handle("/notifications/album", jwtMiddleware(h.NotificationsEndpointHandler(ctx, connPool, rdb))).Methods("POST", "DELETE")  // Protected
	r.Handle("/notifications/friend", jwtMiddleware(h.NotificationsEndpointHandler(ctx, connPool, rdb))).Methods("POST", "DELETE") // Protected

	//Start Server
	fmt.Printf("Server is starting on %v...\n", serverString)
	err = http.ListenAndServe(serverString, r)
	if err != nil {
		fmt.Printf("Error starting the server: %v\n", err)
	}

}
