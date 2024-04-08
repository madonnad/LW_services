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
			DELETEDenyAlbumRequest(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
		}
	})
}

func PUTAcceptAlbumRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, authZeroID string) {
	notification := m.AlbumRequestNotification{
		Status: `accepted`,
	}

	wsPayload := WebSocketPayload{
		Operation: "ACCEPTED",
		Type:      "album-invite",
	}

	requestID := r.URL.Query().Get("request_id")

	updateReqToAccepted := `UPDATE album_requests
							SET invite_seen = true, status = 'accepted', updated_at = (now() AT TIME ZONE 'utc'::text) 
							WHERE request_id = $1
							RETURNING album_id`

	addUserToAlbumUser := `INSERT INTO albumuser (album_id, user_id) 
							VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2))`

	acceptsInfoQuery := `SELECT user_id, first_name, last_name from users WHERE auth_zero_id = $1`
	albumInfoQuery := `SELECT album_name, album_cover_id, album_owner FROM albums WHERE album_id = $1`
	addNotificationForOwner := `INSERT INTO notifications (album_id, media_id, sender_id, receiver_id, type)
								VALUES ($1, $2, $3, $4, 'album_accepted')
								RETURNING notification_uid, received_at, seen`

	err := connPool.Pool.QueryRow(ctx, updateReqToAccepted, requestID).Scan(&notification.AlbumID)
	if err != nil {
		log.Printf("Update Request Error: %v", err)
		return
	}

	_, err = connPool.Pool.Exec(ctx, addUserToAlbumUser, notification.AlbumID, authZeroID)
	if err != nil {
		log.Printf("Add User to AU Error: %v", err)
		return
	}

	batch := &pgx.Batch{}
	batch.Queue(acceptsInfoQuery, authZeroID)
	batch.Queue(albumInfoQuery, notification.AlbumID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)

	err = batchResults.QueryRow().Scan(&notification.GuestID, &notification.GuestFirst, &notification.GuestLast)
	if err != nil {
		log.Print(err)
	}
	err = batchResults.QueryRow().Scan(&notification.AlbumName, &notification.AlbumCoverID, &wsPayload.UserID)
	if err != nil {
		log.Print(err)
	}
	err = connPool.Pool.QueryRow(ctx, addNotificationForOwner, notification.AlbumID, notification.AlbumCoverID,
		notification.GuestID, wsPayload.UserID).Scan(&notification.RequestID, &notification.ReceivedAt, &notification.RequestSeen)
	if err != nil {
		log.Print(err)
	}

	wsPayload.Payload = notification

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

func DELETEDenyAlbumRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, authZeroID string) {
	response := m.AlbumRequestNotification{
		Status: `denied`,
	}
	wsPayload := WebSocketPayload{
		Operation: "DENIED",
		Type:      "album-invite",
	}
	requestID := r.URL.Query().Get("request_id")

	// Prepare SQL statements
	denyRequestQuery := `UPDATE album_requests 
							SET status = 'denied', invite_seen = true , updated_at = (now() AT TIME ZONE 'utc'::text) 
							WHERE request_id = $1 
							RETURNING album_id, updated_at`
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

	err = batchResults.QueryRow().Scan(&response.GuestID, &response.GuestFirst, &response.GuestLast)
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
