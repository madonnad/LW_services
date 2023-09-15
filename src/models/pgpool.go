package models

import (
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PGPool struct {
	Pool *pgxpool.Pool
}

func (connPool PGPool) GETHandlerRoot(w http.ResponseWriter, r *http.Request) {
	var welcomeString string = fmt.Sprintln("Welcome to LW Services.\nRequest one of the following routes to query data:\n /users\n /albums\n /ws")
	responseBytes := []byte(welcomeString)

	w.Header().Set("Content-Type", "text/plain")
	w.Write(responseBytes)
}
