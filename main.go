package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	host     = "0.0.0.0"
	port     = 5432
	user     = "dmadonna"
	password = "1425"
	dbname   = "nw_db"
)

type PGPool struct {
	pool *pgxpool.Pool
}

type WSConn struct {
	conn *websocket.Conn
	uid  uuid.UUID
}

type WSPool struct {
	pool []WSConn
}

func CreatePostgresPool(connString string) (*PGPool, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)

	if err != nil {
		log.Print(err)
		return nil, err
	}

	return &PGPool{pool: pool}, nil
}

func main() {
	connString := fmt.Sprintf("user=%v password=%v host=%v port=%v dbname=%v", user, password, host, port, dbname)
	connPool, _ := CreatePostgresPool(connString)

	host := "0.0.0.0"
	port := "2525"
	serverString := fmt.Sprintf("%v:%v", host, port)

	var wsPool WSPool
	wsPool.pool = make([]WSConn, 0)

	//Route Register
	http.HandleFunc("/", connPool.GETHandlerRoot)
	http.HandleFunc("/ws", wsPool.WebSocketHandler)
	http.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			connPool.GETUserInformation(w, r)
		}
	})
	http.HandleFunc("/albums", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			connPool.GETAlbumsByUID(w, r)
		case http.MethodPost:
			connPool.POSTNewAlbum(w, r)
		}
	})

	//Start Server
	fmt.Printf("Server is starting on %v...\n", serverString)
	err := http.ListenAndServe(serverString, nil)
	if err != nil {
		fmt.Printf("Error starting the server: %v\n", err)
	}

}

func (connPool *PGPool) GETHandlerRoot(w http.ResponseWriter, r *http.Request) {
	var welcomeString string = fmt.Sprintln("Welcome to LW Services.\nRequest one of the following routes to query data:\n /user")
	responseBytes := []byte(welcomeString)

	w.Header().Set("Content-Type", "text/plain")
	w.Write(responseBytes)
}
