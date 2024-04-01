package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	m "last_weekend_services/src/models"
	"log"
	"net/http"
)

func FriendRequestHandler(ctx context.Context, connPool *m.PGPool, rdb *redis.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			fmt.Fprintf(w, "Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodPost:
			POSTFriendRequest(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
		case http.MethodPut:
			PUTAcceptFriendRequest(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
		case http.MethodDelete:
			DELETEDenyFriendRequest(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
		}
	})
}

func POSTFriendRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, senderID string) {
	var friendRequest m.FriendRequestNotification
	receivingID := r.URL.Query().Get("id")
	wsPayload := WebSocketPayload{
		Operation: "REQUEST",
		Type:      "friend-request",
		UserID:    receivingID,
	}

	// Create SQL entry to add request to friend request table
	requestQuery := `INSERT INTO friend_requests (sender_id, receiver_id) 
					 VALUES ((SELECT user_id FROM users WHERE auth_zero_id=$1), $2)
					 RETURNING requested_at`
	senderInfoQuery := `SELECT user_id, first_name, last_name from users WHERE auth_zero_id = $1`

	batch := &pgx.Batch{}
	batch.Queue(requestQuery, senderID, receivingID)
	batch.Queue(senderInfoQuery, senderID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)

	err := batchResults.QueryRow().Scan(&friendRequest.ReceivedAt)
	if err != nil {
		WriteErrorToWriter(w, "Error: Unable to add friend request")
		log.Printf("Unable to add friend request: %v", err)
		return
	}

	err = batchResults.QueryRow().Scan(&friendRequest.UserID, &friendRequest.FirstName, &friendRequest.LastName)
	if err != nil {
		WriteErrorToWriter(w, "Error: Unable to lookup requesting user")
		log.Printf("Unable to lookup requesting user: %v", err)
		return
	}

	wsPayload.Payload = friendRequest

	// If successfully added to friend_request table then publish the change to redis
	jsonPayload, err := json.MarshalIndent(wsPayload, "", "\t")
	if err != nil {
		log.Print(err)
	}

	err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	//Respond to the calling user that the action was successful
	responseBytes, err := json.MarshalIndent("friend request sent - success", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}

func PUTAcceptFriendRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, usersID string) {
	var friendRequest m.FriendRequestNotification
	requestersID := r.URL.Query().Get("id")
	wsPayload := WebSocketPayload{
		Operation: "ACCEPTED",
		Type:      "friend-request",
		UserID:    requestersID,
	}

	removeReqFromTable := `DELETE FROM friend_requests 
       						WHERE sender_id = $1 
       							AND receiver_id = (SELECT user_id FROM users WHERE auth_zero_id=$2)`

	addFriendshipQuery := `INSERT INTO friends (user1_id, user2_id)
							VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2))
							RETURNING friends_since`

	senderInfoQuery := `SELECT user_id, first_name, last_name from users WHERE auth_zero_id = $1`

	// Remove from Friend Request Table
	_, err := connPool.Pool.Exec(ctx, removeReqFromTable, requestersID, usersID)
	if err != nil {
		fmt.Fprintf(w, "Error trying to remove request: %v", err)
		return
	}

	err = connPool.Pool.QueryRow(ctx, addFriendshipQuery, requestersID, usersID).Scan(&friendRequest.ReceivedAt)
	if err != nil {
		fmt.Fprintf(w, "Error trying to insert friend to friends list: %v", err)
		return
	}

	err = connPool.Pool.QueryRow(ctx, senderInfoQuery, usersID).Scan(&friendRequest.UserID, &friendRequest.FirstName, &friendRequest.LastName)
	if err != nil {
		fmt.Fprintf(w, "Unable to lookup requesting user: %v", err)
		return
	}

	wsPayload.Payload = friendRequest

	// If successfully added to friend_request table then publish the change to redis
	jsonPayload, err := json.MarshalIndent(wsPayload, "", "\t")
	if err != nil {
		log.Print(err)
	}

	err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	//Respond to the calling user that the action was successful
	responseBytes, err := json.MarshalIndent("friend request accepted", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}

func DELETEDenyFriendRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, usersID string) {
	sendersID := r.URL.Query().Get("id")

	removeReqFromTable := `DELETE FROM friend_requests 
       						WHERE sender_id = $1 
       							AND receiver_id = (SELECT user_id FROM users WHERE auth_zero_id=$2)`
	_, err := connPool.Pool.Exec(ctx, removeReqFromTable, sendersID, usersID)
	if err != nil {
		fmt.Fprintf(w, "Error trying to remove request: %v", err)
		return
	}

	//Respond to the calling user that the action was successful
	responseBytes, err := json.MarshalIndent("friend request denied", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}
