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

func AlbumRequestHandler(ctx context.Context, connPool *m.PGPool, rdb *redis.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			fmt.Fprintf(w, "Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodPut:
			PUTAcceptAlbumRequest(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
		case http.MethodDelete:
			DeleteDenyAlbumRequest(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
		}
	})
}

func PUTAcceptAlbumRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, authZeroID string) {
	response := m.AlbumRequestNotification{
		Status: `accepted`,
	}

	wsPayload := WebSocketPayload{
		Operation: "ACCEPTED",
		Type:      "album-request",
	}

	requestID := r.URL.Query().Get("request_id")

	updateReqToAccepted := `UPDATE album_requests
							SET invite_seen = true, status = 'accepted', updated_at = (now() AT TIME ZONE 'utc'::text) 
							WHERE request_id = $1
							RETURNING album_id`

	addUserToAlbumUser := `INSERT INTO albumuser (album_id, user_id) 
							VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2)) 
							RETURNING accepted_at`

	acceptsInfoQuery := `SELECT user_id, first_name, last_name from users WHERE auth_zero_id = $1`
	albumInfoQuery := `SELECT album_cover_id, album_name, album_owner, unlocked_at FROM albums WHERE album_id = $1`

	err := connPool.Pool.QueryRow(ctx, updateReqToAccepted, requestID).Scan(&response.AlbumID)
	if err != nil {
		log.Printf("Update Request Error: %v", err)
		return
	}

	err = connPool.Pool.QueryRow(ctx, addUserToAlbumUser, response.AlbumID, authZeroID).Scan(&response.ReceivedAt)
	if err != nil {
		log.Printf("Add User to AU Error: %v", err)
		return
	}

	batch := &pgx.Batch{}
	batch.Queue(acceptsInfoQuery, authZeroID)
	batch.Queue(albumInfoQuery, response.AlbumID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)

	err = batchResults.QueryRow().Scan(&response.ReceiverID, &response.ReceiverFirst, &response.ReceiverLast)
	if err != nil {
		log.Print(err)
	}
	err = batchResults.QueryRow().Scan(&response.AlbumCoverID, &response.AlbumName, &wsPayload.UserID, &response.UnlockedAt)
	if err != nil {
		log.Print(err)
	}

	wsPayload.Payload = response

	// Send payload to WebSocket
	jsonPayload, err := json.MarshalIndent(wsPayload, "", "\t")
	if err != nil {
		log.Print(err)
	}

	err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	//Respond to the calling user that the action was successful
	responseBytes, err := json.MarshalIndent("album invite accepted - success", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)

}

func DeleteDenyAlbumRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, authZeroID string) {
	response := m.AlbumRequestNotification{
		Status: `denied`,
	}
	wsPayload := WebSocketPayload{
		Operation: "DENIED",
		Type:      "album-request",
	}
	requestID := r.URL.Query().Get("request_id")

	// Prepare SQL statements
	denyRequestQuery := `UPDATE album_requests 
							SET status = 'denied', updated_at = (now() AT TIME ZONE 'utc'::text) 
							WHERE request_id = $1 RETURNING album_id`
	albumOwnerIDQuery := `SELECT album_owner FROM albums WHERE album_id = $1`
	userInfoQuery := `SELECT user_id, first_name, last_name FROM users WHERE auth_zero_id = $1`

	// Execute the delete outside of the batch since the albumOwnerIDQuery is reliant on the response of this request
	err := connPool.Pool.QueryRow(ctx, denyRequestQuery, requestID).Scan(&response.AlbumID, &response.ReceivedAt)
	if err != nil {
		log.Print(err)
		return
	}

	// Batch remaining queries
	batch := &pgx.Batch{}
	batch.Queue(albumOwnerIDQuery, &response.AlbumID)
	batch.Queue(userInfoQuery, authZeroID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)

	err = batchResults.QueryRow().Scan(&wsPayload.UserID)
	if err != nil {
		log.Print(err)
		return
	}

	err = batchResults.QueryRow().Scan(&response.ReceiverID, &response.ReceiverFirst, &response.ReceiverLast)
	if err != nil {
		log.Print(err)
		return
	}

	// Add the response struct to the wsPayload
	wsPayload.Payload = response

	// Send payload to WebSocket
	jsonPayload, err := json.MarshalIndent(wsPayload, "", "\t")
	if err != nil {
		log.Print(err)
	}

	err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	//Respond to the calling user that the action was successful
	responseBytes, err := json.MarshalIndent("album invite denied - success", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}
