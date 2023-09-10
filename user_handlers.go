package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type User struct {
	ID        string    `json:"user_id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	CreatedAt time.Time `json:"created_at"`
	Avatar    string    `json:"avatar"`
}

func (connPool *PGPool) GETUserInformation(w http.ResponseWriter, r *http.Request) {
	var user User

	uid, err := uuid.Parse(r.URL.Query().Get("uid"))
	if err != nil {
		var errorString string = fmt.Sprintln("Error: Provide a unique, valid UUID to return a user")
		responseBytes := []byte(errorString)

		w.Header().Set("Content-Type", "text/plain")
		w.Write(responseBytes)
		return
	}

	sql_query := "SELECT * FROM users WHERE user_id=$1"
	response := connPool.pool.QueryRow(context.Background(), sql_query, uid)

	//fmt.Printf("%v", response.Scan())
	err = response.Scan(&user.ID, &user.CreatedAt, &user.FirstName, &user.LastName, &user.Avatar)
	if err != nil {
		if err == pgx.ErrNoRows {
			var errorString string = fmt.Sprintln("Error: User does not exist")
			responseBytes := []byte(errorString)

			w.Header().Set("Content-Type", "text/plain")
			w.Write(responseBytes)
			return
		} else {
			log.Panic(err)
			return
		}

	}

	responseBytes, err := json.MarshalIndent(user, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)

}
