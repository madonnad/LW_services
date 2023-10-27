package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"log"
	"net/http"

	m "last_weekend_services/src/models"

	"github.com/jackc/pgx/v5"
)

func UserEndpointHandler(connPool *m.PGPool) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodGet:
			GETAuthUserInformation(w, r, connPool, claims.RegisteredClaims.Subject)
		}
	})
}

func GETAuthUserInformation(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	var user m.User

	sql_query := "SELECT user_id, created_at, first_name, last_name FROM users WHERE auth_zero_id=$1"
	response := connPool.Pool.QueryRow(context.Background(), sql_query, uid)

	//fmt.Printf("%v", response.Scan())
	err := response.Scan(&user.ID, &user.CreatedAt, &user.FirstName, &user.LastName)
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
