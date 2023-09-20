package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	h "last_weekend_services/src/handlers"
	m "last_weekend_services/src/models"

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

	//Server Starting String
	host := "0.0.0.0"
	port := "2525"
	serverString := fmt.Sprintf("%v:%v", host, port)

	//Route Register
	http.HandleFunc("/", connPool.GETHandlerRoot)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		h.WebSocketHandler(w, r, connPool, rdb, ctx)
	})
	http.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.GETUserInformation(w, r, connPool)
		}
	})
	http.HandleFunc("/albums", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.GETAlbumsByUID(w, r, connPool)
		case http.MethodPost:
			h.POSTNewAlbum(ctx, w, r, connPool, rdb)
		}
	})
	http.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/images/ofuser" {
			h.GETImagesFromUserID(w, r, connPool, ctx)
			return
		}

		switch r.Method {
		case http.MethodGet:
			h.GETImageFromID(w, r, connPool, ctx)
		case http.MethodPost:
			h.POSTNewImage(w, r, connPool, ctx)
		}
	})

	//Start Server
	fmt.Printf("Server is starting on %v...\n", serverString)
	err := http.ListenAndServe(serverString, nil)
	if err != nil {
		fmt.Printf("Error starting the server: %v\n", err)
	}

}
