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
			DELETEDenyFriendRequest(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
		case http.MethodPatch:
			PATCHFriendRequestSeen(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
		}
	})
}

func POSTFriendRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, senderID string) {
	var friendRequest m.FriendRequestNotification
	friendRequest.ReceiverID = r.URL.Query().Get("id")
	wsPayload := WebSocketPayload{
		Operation: "REQUEST",
		Type:      "friend-request",
		UserID:    friendRequest.ReceiverID,
	}

	// Create SQL entry to add request to friend request table
	requestQuery := `INSERT INTO friend_requests (sender_id, receiver_id) 
					 VALUES ((SELECT user_id FROM users WHERE auth_zero_id=$1), $2)
					 RETURNING request_id, updated_at`
	senderInfoQuery := `SELECT user_id, first_name, last_name from users WHERE auth_zero_id = $1`

	batch := &pgx.Batch{}
	batch.Queue(requestQuery, senderID, friendRequest.ReceiverID)
	batch.Queue(senderInfoQuery, senderID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)
	defer func() {
		err := batchResults.Close()
		if err != nil {
			log.Printf("%v", err)
			return
		}
	}()

	err := batchResults.QueryRow().Scan(&friendRequest.RequestID, &friendRequest.ReceivedAt)
	if err != nil {
		WriteErrorToWriter(w, "Error: Unable to add friend request")
		log.Printf("Unable to add friend request: %v", err)
		return
	}

	err = batchResults.QueryRow().Scan(&friendRequest.SenderID, &friendRequest.FirstName, &friendRequest.LastName)
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
	friendRequest.SenderID = r.URL.Query().Get("id")
	friendRequest.RequestID = r.URL.Query().Get("request_id")
	wsPayload := WebSocketPayload{
		Operation: "ACCEPTED",
		Type:      "friend-request",
		UserID:    friendRequest.SenderID,
	}

	updateReqToAccepted := `UPDATE friend_requests
							SET status = 'accepted', updated_at = (now() AT TIME ZONE 'utc'::text), seen = true
							WHERE request_id = $1
							RETURNING status, seen`

	addFriendshipQuery := `INSERT INTO friends (user1_id, user2_id)
							VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2))
							RETURNING friends_since`

	senderInfoQuery := `SELECT user_id, first_name, last_name from users WHERE auth_zero_id = $1`

	// Update Entry in Friend Request Table
	err := connPool.Pool.QueryRow(ctx, updateReqToAccepted, friendRequest.RequestID).Scan(&friendRequest.Status, &friendRequest.RequestSeen)
	if err != nil {
		fmt.Fprintf(w, "Error trying to remove request: %v", err)
		return
	}

	err = connPool.Pool.QueryRow(ctx, addFriendshipQuery, friendRequest.SenderID, usersID).Scan(&friendRequest.ReceivedAt)
	if err != nil {
		fmt.Fprintf(w, "Error trying to insert friend to friends list: %v", err)
		return
	}

	err = connPool.Pool.QueryRow(ctx, senderInfoQuery, usersID).Scan(&friendRequest.ReceiverID, &friendRequest.FirstName, &friendRequest.LastName)
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

func DELETEDenyFriendRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, usersAuthZeroID string) {
	requestID := r.URL.Query().Get("id")

	removeReqFromTable := `DELETE FROM friend_requests 
       						WHERE request_id = $1`
	_, err := connPool.Pool.Exec(ctx, removeReqFromTable, requestID)
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

func PATCHFriendRequestSeen(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, usersAuthZeroID string) {
	requestID := r.URL.Query().Get("id")

	updateSeenStatus := `UPDATE friend_requests
						SET seen = true
						WHERE request_id = $1`

	_, err := connPool.Pool.Exec(ctx, updateSeenStatus, requestID)
	if err != nil {
		fmt.Fprintf(w, "Error trying to mark friend request as seen: %v", err)
		return
	}

	responseBytes, err := json.MarshalIndent("friend request successfully seen", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}
