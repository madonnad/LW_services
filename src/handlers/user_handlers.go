package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/opensearch-project/opensearch-go"
	"github.com/opensearch-project/opensearch-go/opensearchapi"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	m "last_weekend_services/src/models"

	"github.com/jackc/pgx/v5"
)

func UserEndpointHandler(connPool *m.PGPool, ctx context.Context, osClient *opensearch.Client) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodPost:
			POSTNewAccount(ctx, w, r, connPool, claims.RegisteredClaims.Subject, osClient)
		case http.MethodGet:
			switch r.URL.Path {
			case "/user":
				GETAuthUserInformation(w, connPool, claims.RegisteredClaims.Subject)
			case "/user/id":
				GETUserByUID(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			}
		}
	})
}

func POSTNewAccount(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, authZeroId string, osClient *opensearch.Client) {
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

	query := `INSERT INTO users (first_name, last_name, auth_zero_id) VALUES ($1, $2, $3) RETURNING user_id`

	result := connPool.Pool.QueryRow(ctx, query, user.FirstName, user.LastName, authZeroId)
	err = result.Scan(&uid)
	if err != nil {
		WriteErrorToWriter(w, "Unable to create new user entry")
		log.Printf("Unable to create new user entry: %v", err)
		return
	}

	// Add to OpenSearch Database
	// Prepare - Prepare struct to be added to opensearch
	name := fmt.Sprintf("%s %s", user.FirstName, user.CreatedAt)
	osUser := m.Search{ID: uid, Name: name, FirstName: user.FirstName, LastName: user.LastName, ResultType: "user"}

	// Format the JSON Format to be Accepted
	data, err := json.MarshalIndent(osUser, "", "\t")
	document := strings.NewReader(string(data))

	// Process Request
	req := opensearchapi.IndexRequest{
		Index:      "global-search",
		DocumentID: osUser.ID,
		Body:       document,
	}
	insertResponse, err := req.Do(ctx, osClient)
	if err != nil {
		fmt.Println("failed to insert document ", err)
		os.Exit(1)
	}
	defer insertResponse.Body.Close()

	// Send success to the client
	responseBytes, err := json.MarshalIndent("account created - success", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
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
