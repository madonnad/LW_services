package main

import (
	"cloud.google.com/go/storage"
	"context"
	firebase "firebase.google.com/go/v4"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	h "last_weekend_services/src/handlers"
	i "last_weekend_services/src/inits"
	"last_weekend_services/src/middleware"
	"log"
	"net/http"
	"os"
	"strconv"
)

func main() {
	ctx := context.Background()

	//Remove when pushing commit - only for local testing
	err := godotenv.Load()
	if err != nil {
		fmt.Println("cannot get env variables:", err)
		os.Exit(1)
	}

	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil {
		port = 8080
		log.Printf("defaulting to port %v", port)
	}

	// Postgres Config Vals
	//dbHost := os.Getenv("DB_HOST")
	unixSocketPath := os.Getenv("INSTANCE_UNIX_SOCKET")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	// Redis Config Vals
	rdbAddr := os.Getenv("RDB_ADDR")
	rdbUser := os.Getenv("RDB_USER")
	rdbPassword := os.Getenv("RDB_PASSWORD")
	rdbNo, err := strconv.Atoi(os.Getenv("RDB_NO"))
	if err != nil {
		rdbNo = 0
		log.Printf("defaulting to db %v", rdbNo)
	}

	// Auth0 Config Vals
	authDomain := os.Getenv("AUTH0_DOMAIN")
	authAudience := os.Getenv("AUTH0_AUDIENCE")

	// GCP Storage Config Vals
	storageBucket := os.Getenv("STORAGE_BUCKET")

	// Postgres Initialization
	connString := fmt.Sprintf("user=%v password=%v host=%v dbname=%v",
		dbUser, dbPassword, unixSocketPath, dbName)
	connPool, err := i.CreatePostgresPool(connString, ctx)
	if err != nil {
		fmt.Println("cannot get postgres instance:", err)
		os.Exit(1)
	}
	defer connPool.Pool.Close()

	// Redis Initialization
	rdb := redis.NewClient(&redis.Options{
		Addr:     rdbAddr,
		Username: rdbUser,
		Password: rdbPassword,
		DB:       rdbNo,
	})

	// GCP Storage Initialization
	gcpStorage, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Initialize Firebase SDK
	config := firebase.Config{
		ProjectID: "lastweekend",
	}
	app, err := firebase.NewApp(ctx, &config)
	if err != nil {
		log.Fatal(err)
	}

	// Initialize Firebase Messaging
	messagingClient, err := app.Messaging(ctx)
	if err != nil {
		log.Fatal(err)
	}

	//Server Starting String
	host := "0.0.0.0"
	serverString := fmt.Sprintf("%v:%v", host, port)

	r := mux.NewRouter()

	jwtMiddleware := middleware.EnsureValidToken(authDomain, authAudience)

	//Route Register
	r.HandleFunc("/", connPool.GETHandlerRoot)                                                                                       // Unprotected
	r.Handle("/ws", jwtMiddleware(h.WebSocketEndpointHandler(connPool, rdb, ctx)))                                                   // Protected
	r.Handle("/ws/album", jwtMiddleware(h.WebSocketEndpointHandler(connPool, rdb, ctx)))                                             // Protected
	r.Handle("/search", jwtMiddleware(h.SearchEndpointHandler(ctx, connPool))).Methods("GET")                                        // Protected
	r.Handle("/feed", jwtMiddleware(h.FeedEndpointHandler(ctx, connPool))).Methods("GET")                                            // Protected
	r.Handle("/image", jwtMiddleware(h.ContentEndpointHandler(ctx, *gcpStorage, storageBucket))).Methods("GET")                      // Protected
	r.Handle("/image/comment", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("GET", "POST", "PATCH", "DELETE")  // Protected
	r.Handle("/image/comment/seen", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("PATCH")                      // Protected
	r.Handle("/image/like", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("POST", "DELETE")                     // Protected
	r.Handle("/image/upvote", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("POST", "DELETE")                   // Protected
	r.Handle("/album", jwtMiddleware(h.AlbumEndpointHandler(connPool, rdb, ctx, messagingClient))).Methods("GET")                    // Protected
	r.Handle("/album/images", jwtMiddleware(h.AlbumEndpointHandler(connPool, rdb, ctx, messagingClient))).Methods("GET")             // Protected
	r.Handle("/album/revealed", jwtMiddleware(h.AlbumEndpointHandler(connPool, rdb, ctx, messagingClient))).Methods("GET")           // Protected
	r.Handle("/album/guests", jwtMiddleware(h.AlbumEndpointHandler(connPool, rdb, ctx, messagingClient))).Methods("GET", "POST")     // Protected
	r.Handle("/upload", jwtMiddleware(h.ContentEndpointHandler(ctx, *gcpStorage, storageBucket))).Methods("GET")                     // Protected
	r.Handle("/user", jwtMiddleware(h.UserEndpointHandler(connPool, ctx))).Methods("GET", "POST")                                    // Protected
	r.Handle("/user/id", jwtMiddleware(h.UserEndpointHandler(connPool, ctx))).Methods("GET")                                         // Protected
	r.Handle("/user/album", jwtMiddleware(h.AlbumEndpointHandler(connPool, rdb, ctx, messagingClient))).Methods("GET", "POST")       // Protected
	r.Handle("/user/album/image", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("GET", "POST")                  // Protected
	r.Handle("/user/recap", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("POST")                               // Protected
	r.Handle("/user/image", jwtMiddleware(h.ImageEndpointHandler(connPool, rdb, ctx))).Methods("GET", "POST", "PATCH")               // Protected
	r.Handle("/user/friend", jwtMiddleware(h.FriendEndpointHandler(ctx, connPool, rdb))).Methods("GET", "DELETE")                    // Protected
	r.Handle("/friend-request", jwtMiddleware(h.FriendRequestHandler(ctx, connPool, rdb))).Methods("POST", "PUT", "DELETE", "PATCH") // Protected
	r.Handle("/album-invite", jwtMiddleware(h.AlbumRequestHandler(ctx, connPool, rdb))).Methods("PUT", "DELETE", "PATCH")            // Protected
	r.Handle("/notifications", jwtMiddleware(h.NotificationsEndpointHandler(ctx, connPool, rdb))).Methods("GET", "PATCH")            // Protected
	r.Handle("/fcm", jwtMiddleware(h.FirebaseHandlers(connPool, ctx))).Methods("PUT")

	//Start Server
	fmt.Printf("Server is starting on %v...\n", serverString)
	err = http.ListenAndServe(serverString, r)
	if err != nil {
		fmt.Printf("Error starting the server: %v\n", err)
	}

}
