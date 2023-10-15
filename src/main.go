package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	h "last_weekend_services/src/handlers"
	middleware "last_weekend_services/src/middleware"
	m "last_weekend_services/src/models"

	"cloud.google.com/go/storage"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	host     = "0.0.0.0"
	port     = 5432
	user     = "dmadonna"
	password = "1425"
	dbname   = "lw_db"
)

func CreatePostgresPool(connString string, context context.Context) (*m.PGPool, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(context, cfg)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	return &m.PGPool{Pool: pool}, nil
}

func main() {
	ctx := context.Background()

	//Auth0 Initialization

	// Postgres Initialization
	connString := fmt.Sprintf("user=%v password=%v host=%v port=%v dbname=%v", user, password, host, port, dbname)
	connPool, _ := CreatePostgresPool(connString, ctx)
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

	//Server Starting String
	host := "0.0.0.0"
	port := "2525"
	serverString := fmt.Sprintf("%v:%v", host, port)

	r := mux.NewRouter()

	jwtMiddleware := middleware.EnsureValidToken()

	//Route Register
	r.HandleFunc("/", connPool.GETHandlerRoot) // Unprotected
	r.Handle("/ws", jwtMiddleware(h.WebSocketEndpointHandler(connPool, rdb, ctx)))
	r.Handle("/feed", jwtMiddleware(h.FeedEndpointHandler(ctx, connPool))).Methods("GET")                                          // Protected
	r.Handle("/user", jwtMiddleware(h.UserEndpointHandler(connPool))).Methods("GET")                                               // Protected
	r.Handle("/user/album", jwtMiddleware(h.AlbumEndpointHandler(connPool, rdb, ctx))).Methods("GET", "POST")                      // Protected
	r.Handle("/user/album/image", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("GET", "POST")                // Protected
	r.Handle("/user/image", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("GET")                              // Protected
	r.Handle("/user/friend", jwtMiddleware(h.FriendEndpointHanlder(ctx, connPool, rdb))).Methods("GET", "DELETE")                  // Protected
	r.Handle("/image", jwtMiddleware(h.ContentEndpointHandler(ctx, *gcpStorage))).Methods("GET")                                   // Protected
	r.Handle("/upload", jwtMiddleware(h.ContentEndpointHandler(ctx, *gcpStorage))).Methods("GET")                                  // Protected
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
