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
		case http.MethodPatch:
			PATCHMarkRequestResponseAsSeen(ctx, w, r, connPool)
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

	var guests []m.Guest

	notification.RequestID = r.URL.Query().Get("request_id")

	updateReqToAccepted := `UPDATE album_requests
							SET invite_seen = true, status = 'accepted', updated_at = (now() AT TIME ZONE 'utc'::text) 
							WHERE request_id = $1
							RETURNING album_id, updated_at`

	addUserToAlbumUser := `INSERT INTO albumuser (album_id, user_id) 
							VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2))`

	acceptsInfoQuery := `SELECT user_id, first_name, last_name from users WHERE auth_zero_id = $1`
	albumInfoQuery := `SELECT a.album_name, a.album_cover_id, a.revealed_at ,a.album_owner, u.first_name, u.last_name
						FROM albums a
						JOIN users u
						ON u.user_id = a.album_owner
						WHERE album_id = $1`
	//addNotificationForOwner := `INSERT INTO notifications (album_id, media_id, sender_id, receiver_id, type)
	//							VALUES ($1, $2, $3, $4, 'album_accepted')
	//							RETURNING notification_uid, received_at, seen`

	//Original Request
	//getGuestIDs := `SELECT user_id FROM albumuser WHERE (album_id = $1
	//                    AND user_id != (SELECT user_id FROM users WHERE users.auth_zero_id = $2))`
	getGuestsIDsAR := `SELECT invited_id
						FROM album_requests
						WHERE (album_id = $1
						AND status = 'accepted'
						AND invited_id != (SELECT user_id FROM users WHERE users.auth_zero_id = $2));`

	err := connPool.Pool.QueryRow(ctx, updateReqToAccepted, notification.RequestID).Scan(&notification.AlbumID, &notification.ReceivedAt)
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
	batch.Queue(getGuestsIDsAR, notification.AlbumID, authZeroID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)
	defer func() {
		err := batchResults.Close()
		if err != nil {
			log.Printf("%v", err)
			return
		}
	}()

	err = batchResults.QueryRow().Scan(&notification.GuestID, &notification.GuestFirst, &notification.GuestLast)
	if err != nil {
		log.Print(err)
	}
	err = batchResults.QueryRow().Scan(&notification.AlbumName, &notification.AlbumCoverID, &notification.RevealedAt,
		&notification.AlbumOwner, &notification.OwnerFirst, &notification.OwnerLast)
	if err != nil {
		log.Print(err)
	}
	//err = connPool.Pool.QueryRow(ctx, addNotificationForOwner, notification.AlbumID, notification.AlbumCoverID,
	//	notification.GuestID, wsPayload.UserID).Scan(&notification.RequestID, &notification.ReceivedAt, &notification.InviteSeen)
	//if err != nil {
	//	log.Print(err)
	//}
	rows, err := batchResults.Query()
	if err != nil {
		log.Print(err)
	}

	for rows.Next() {
		var guest m.Guest

		err = rows.Scan(&guest.ID)
		if err != nil {
			log.Print(err)
		}

		guests = append(guests, guest)
	}

	wsPayload.Payload = notification

	for _, guest := range guests {
		wsPayload.UserID = guest.ID

		// Send payload to WebSocket
		jsonPayload, err := json.MarshalIndent(wsPayload, "", "\t")
		if err != nil {
			log.Print(err)
		}

		err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
		if err != nil {
			log.Print(err)
		}

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
	notification := m.AlbumRequestNotification{
		Status: `denied`,
	}
	wsPayload := WebSocketPayload{
		Operation: "DENIED",
		Type:      "album-invite",
	}
	requestID := r.URL.Query().Get("request_id")
	var guests []m.Guest

	// Prepare SQL statements
	denyRequestQuery := `UPDATE album_requests 
							SET status = 'denied', invite_seen = true, response_seen = true, updated_at = (now() AT TIME ZONE 'utc'::text)
							WHERE request_id = $1 
							RETURNING album_id, updated_at`
	albumOwnerIDQuery := `SELECT album_owner FROM albums WHERE album_id = $1`
	userInfoQuery := `SELECT user_id, first_name, last_name FROM users WHERE auth_zero_id = $1`

	//Original Guest Query
	//getGuestIDs := `SELECT user_id FROM albumuser WHERE (album_id = $1
	//                                     AND user_id != (SELECT user_id FROM users WHERE users.auth_zero_id = $2))`
	getGuestsIDsAR := `SELECT invited_id
						FROM album_requests
						WHERE (album_id = $1
						AND status = 'accepted'
						AND invited_id != (SELECT user_id FROM users WHERE users.auth_zero_id = $2));`

	// Execute the delete outside of the batch since the albumOwnerIDQuery is reliant on the notification of this request
	err := connPool.Pool.QueryRow(ctx, denyRequestQuery, requestID).Scan(&notification.AlbumID, &notification.ReceivedAt)
	if err != nil {
		log.Print(err)
		return
	}

	// Batch remaining queries
	batch := &pgx.Batch{}
	batch.Queue(albumOwnerIDQuery, &notification.AlbumID)
	batch.Queue(userInfoQuery, authZeroID)
	batch.Queue(getGuestsIDsAR, notification.AlbumID, authZeroID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)
	defer func() {
		err := batchResults.Close()
		if err != nil {
			log.Printf("%v", err)
			return
		}
	}()

	err = batchResults.QueryRow().Scan(&wsPayload.UserID)
	if err != nil {
		log.Print(err)
		return
	}

	err = batchResults.QueryRow().Scan(&notification.GuestID, &notification.GuestFirst, &notification.GuestLast)
	if err != nil {
		log.Print(err)
		return
	}

	rows, err := batchResults.Query()
	if err != nil {
		log.Print(err)
	}

	for rows.Next() {
		var guest m.Guest

		err = rows.Scan(&guest.ID)
		if err != nil {
			log.Print(err)
		}

		guests = append(guests, guest)
	}

	// Add the notification struct to the wsPayload
	wsPayload.Payload = notification

	// Send payload to WebSocket
	for _, guest := range guests {
		wsPayload.UserID = guest.ID

		// Send payload to WebSocket
		jsonPayload, err := json.MarshalIndent(wsPayload, "", "\t")
		if err != nil {
			log.Print(err)
		}

		err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
		if err != nil {
			log.Print(err)
		}

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

func PATCHMarkRequestResponseAsSeen(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool) {
	requestID := r.URL.Query().Get("id")

	markSeenQuery := `UPDATE album_requests
						SET response_seen = true, updated_at = (now() AT TIME ZONE 'utc'::text)
						WHERE request_id = $1`
	_, err := connPool.Pool.Exec(ctx, markSeenQuery, requestID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		responseBytes, _ := json.MarshalIndent("marking album request response as seen failed", "", "\t")

		w.Header().Set("Content-Type", "application/json")
		w.Write(responseBytes)
	}

	responseBytes, err := json.MarshalIndent("album request response seen - success", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}
