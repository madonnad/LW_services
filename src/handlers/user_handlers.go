package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"io"
	m "last_weekend_services/src/models"
	"log"
	"net/http"

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
			switch r.URL.Path {
			case "/user":
				POSTNewAccount(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			}
		case http.MethodGet:
			switch r.URL.Path {
			case "/user":
				GETAuthUserInformation(w, connPool, claims.RegisteredClaims.Subject)
			case "/user/id":
				GETUserByUID(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			}
		case http.MethodPatch:
			switch r.URL.Path {
			case "/user":
				PATCHAuthUserInfo(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			}
		}

	})
}

func POSTNewAccount(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, authZeroId string) {
	var user m.User
	var uid string

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

	query := `INSERT INTO users (first_name, last_name, auth_zero_id, email, tsv_fullname, tsv_email) 
					VALUES ($1, $2, $3, $4, to_tsvector($1 || ' ' || $2), to_tsvector($4)) 
					RETURNING user_id`

	result := connPool.Pool.QueryRow(ctx, query, user.FirstName, user.LastName, authZeroId, &user.Email)
	err = result.Scan(&uid)
	if err != nil {
		WriteErrorToWriter(w, "Unable to create new user entry")
		log.Printf("Unable to create new user entry: %v", err)
		return
	}

	// Send success to the client
	responseBytes, err := json.MarshalIndent("account created - success", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}

func PATCHAuthUserInfo(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, authZeroId string) {
	firstName := r.URL.Query().Get("first")
	lastName := r.URL.Query().Get("last")

	updateQuery := `UPDATE users 
					SET first_name=$1, last_name=$2
					WHERE auth_zero_id=$3`

	tag, err := connPool.Pool.Exec(ctx, updateQuery, firstName, lastName, authZeroId)
	if err != nil || tag.RowsAffected() == 0 {
		log.Printf("Error updating auth user info: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Error updating auth user info"))
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Auth user info updated"))
}

func GETAuthUserInformation(w http.ResponseWriter, connPool *m.PGPool, uid string) {
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

func GETUserByUID(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, authUserID string) {
	friendID := r.URL.Query().Get("id")
	searchResult := m.SearchedUser{ID: friendID}
	var albumIDs []string
	batch := &pgx.Batch{}

	//Name Query
	nameQuery := `SELECT first_name, last_name FROM users WHERE user_id = $1`
	batch.Queue(nameQuery, friendID)

	// Friend Status Query
	friendStatusQuery := `SELECT COALESCE(
							(
							SELECT 'friends'
							FROM friends
							WHERE (
							    user1_id = (SELECT user_id FROM users WHERE auth_zero_id=$1) AND user2_id = $2) OR 
							    (user1_id = $2 AND user2_id = (SELECT user_id FROM users WHERE auth_zero_id=$1))
							),
							(
							SELECT 'pending'
							FROM friend_requests
							WHERE (
							    sender_id = (SELECT user_id FROM users WHERE auth_zero_id=$1) AND receiver_id = $2) OR 
							    (sender_id = $2 AND receiver_id = (SELECT user_id FROM users WHERE auth_zero_id=$1))
							),
							'not friends'
							) as status;`
	batch.Queue(friendStatusQuery, authUserID, friendID)

	// Friend Count Query
	friendCountQuery := `SELECT COUNT(*)
					FROM friends
					WHERE user1_id = $1 OR user2_id = $1;`
	batch.Queue(friendCountQuery, friendID)

	// Revealed Albums Query
	usersRevealedAlbumQuery := `SELECT a.album_id
								FROM albumuser au
								JOIN albums a ON au.album_id = a.album_id
								WHERE au.user_id = $1
								AND a.revealed_at < CURRENT_DATE;`
	batch.Queue(usersRevealedAlbumQuery, friendID)

	// Execute the batch
	batchResults := connPool.Pool.SendBatch(ctx, batch)
	defer func() {
		err := batchResults.Close()
		if err != nil {
			log.Printf("%v", err)
			return
		}
	}()

	// Query the Name Results
	row := batchResults.QueryRow()
	err := row.Scan(&searchResult.FirstName, &searchResult.LastName)
	if err != nil {
		log.Print(err)
	}

	row = batchResults.QueryRow()
	err = row.Scan(&searchResult.FriendStatus)
	if err != nil {
		log.Print(err)
	}

	row = batchResults.QueryRow()
	err = row.Scan(&searchResult.FriendCount)
	if err != nil {
		log.Print(err)
	}

	rows, err := batchResults.Query()
	if err != nil {
		log.Print(err)

	}
	for rows.Next() {
		var albumID string
		err = rows.Scan(&albumID)
		if err != nil {
			log.Print(err)
		}
		albumIDs = append(albumIDs, albumID)
	}
	searchResult.AlbumIDs = albumIDs

	responseBytes, err := json.MarshalIndent(searchResult, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}
