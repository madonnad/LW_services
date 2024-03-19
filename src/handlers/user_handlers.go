package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"io"
	"log"
	"net/http"

	m "last_weekend_services/src/models"

	"github.com/jackc/pgx/v5"
)

func UserEndpointHandler(connPool *m.PGPool, ctx context.Context) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodPost:
			POSTNewAccount(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
		case http.MethodGet:
			GETAuthUserInformation(w, r, connPool, claims.RegisteredClaims.Subject)
		}
	})
}

func POSTNewAccount(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	var user m.User

	bytes, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not read the request body")
		log.Printf("Failed Reading Body: %v", err)
		return
	}

	err = json.Unmarshal(bytes, &user)
	if err != nil {
		WriteErrorToWriter(w, "Error: Invalid request body - could not be mapped to object")
		log.Printf("Failed Unmarshaling: %v", err)
		return
	}

	query := `INSERT INTO users (first_name, last_name, auth_zero_id) VALUES ($1, $2, $3)`

	_, err = connPool.Pool.Exec(ctx, query, user.FirstName, user.LastName, uid)
	if err != nil {
		WriteErrorToWriter(w, "Unable to create new user entry")
		log.Printf("Unable to create new user entry: %v", err)
		return
	}

	responseBytes, err := json.MarshalIndent("account created - success", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}

func GETAuthUserInformation(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	var user m.User

	sqlQuery := "SELECT user_id, created_at, first_name, last_name FROM users WHERE auth_zero_id=$1"
	response := connPool.Pool.QueryRow(context.Background(), sqlQuery, uid)

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
