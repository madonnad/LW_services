package handlers

import (
	"context"
	"encoding/json"
	"firebase.google.com/go/v4/messaging"
	"github.com/jackc/pgx/v5"
	"io"
	m "last_weekend_services/src/models"
	"log"
	"net/http"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/redis/go-redis/v9"
)

func AlbumEndpointHandler(connPool *m.PGPool, rdb *redis.Client, ctx context.Context, messagingClient *messaging.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodGet:
			switch r.URL.Path {
			case "/user/album":
				GETAlbumsByUserID(w, r, connPool, claims.RegisteredClaims.Subject, ctx)
			case "/album":
				GETAlbumByAlbumID(w, r, connPool, ctx, claims.RegisteredClaims.Subject)
			case "/album/images":
				GETAlbumImagesByID(w, r, connPool, ctx, claims.RegisteredClaims.Subject)
			case "/album/revealed":
				GETRevealedAlbumsByAlbumID(w, r, connPool, ctx)
			case "/album/guests":
				GETAlbumGuests(w, r, connPool, ctx)
			}

		case http.MethodPost:
			switch r.URL.Path {
			case "/user/album":
				POSTNewAlbum(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject, messagingClient)
			case "/album/guests":
				InviteUserToAlbum(ctx, w, r, rdb, connPool, messagingClient)
			}
		case http.MethodPatch:
			switch r.URL.Path {
			case "/album/visibility":
				PATCHAlbumVisibility(ctx, w, r, connPool)
			}
		}
	})
}

func GETAlbumByAlbumID(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context, authZeroID string) {
	var album m.Album
	var guests []m.Guest
	batch := &pgx.Batch{}
	albumID := r.URL.Query().Get("album_id")

	albumQuery := `SELECT a.album_id, album_name, album_owner, u.first_name, u.last_name, a.created_at, locked_at, unlocked_at, revealed_at, album_cover_id, visibility
					  FROM albums a
					  JOIN albumuser au
					  ON au.album_id=a.album_id
					  JOIN users u
					  ON a.album_owner=u.user_id
					  WHERE a.album_id=$1`

	guestQuery := `SELECT u.user_id,  u.first_name, u.last_name, ar.status
					FROM users u
					JOIN album_requests ar
					ON u.user_id = ar.invited_id
					WHERE ar.album_id = $1`

	batch.Queue(albumQuery, albumID)
	batch.Queue(guestQuery, albumID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)
	defer func() {
		err := batchResults.Close()
		if err != nil {
			log.Printf("%v", err)
			return
		}
	}()

	err := batchResults.QueryRow().Scan(&album.AlbumID, &album.AlbumName, &album.AlbumOwner, &album.OwnerFirst, &album.OwnerLast,
		&album.CreatedAt, &album.LockedAt, &album.UnlockedAt, &album.RevealedAt, &album.AlbumCoverID, &album.Visibility)
	if err != nil {
		log.Print(err)
		return
	}

	QueryImagesData(ctx, connPool, &album, authZeroID)

	guestRows, err := batchResults.Query()
	if err != nil {
		log.Print(err)
	}
	guest := m.Guest{
		ID:        album.AlbumOwner,
		FirstName: album.OwnerFirst,
		LastName:  album.OwnerLast,
		Status:    "accepted",
	}
	guests = append(guests, guest)

	for guestRows.Next() {
		err = guestRows.Scan(&guest.ID, &guest.FirstName, &guest.LastName, &guest.Status)
		if err != nil {
			log.Print(err)
		}

		guests = append(guests, guest)
	}

	album.InviteList = guests

	err = album.PhaseCalculation()

	responseBytes, err := json.MarshalIndent(album, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(responseBytes)
	if err != nil {
		log.Printf("Failed to Write: %v", err)
		return
	}
}

func GETAlbumImagesByID(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context, authZeroID string) {
	var album m.Album

	album.AlbumID = r.URL.Query().Get("album_id")

	QueryImagesData(ctx, connPool, &album, authZeroID)

	responseBytes, err := json.MarshalIndent(album.Images, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)

}

func GETRevealedAlbumsByAlbumID(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context) {
	var albums []m.Album
	var albumIDs []string

	err := json.NewDecoder(r.Body).Decode(&albumIDs)
	if err != nil {
		WriteErrorToWriter(w, "Error: Invalid request body - could not be mapped to object")
		log.Printf("Unable to decode albumIDs: %v", err)
		return
	}

	albumQuery := `SELECT a.album_id, album_name, album_owner, u.first_name, u.last_name, a.created_at, locked_at, unlocked_at, revealed_at, album_cover_id, visibility
					  FROM albums a
					  JOIN albumuser au
					  ON au.album_id=a.album_id
					  JOIN users u
					  ON a.album_owner=u.user_id
					  WHERE a.album_id=$1 AND a.revealed_at < CURRENT_DATE`

	guestQuery := `SELECT u.user_id,  u.first_name, u.last_name, ar.status
					FROM users u
					JOIN album_requests ar
					ON u.user_id = ar.invited_id
					WHERE ar.album_id = $1`

	for _, id := range albumIDs {
		var album m.Album
		var guests []m.Guest

		err = connPool.Pool.QueryRow(ctx, albumQuery, id).Scan(&album.AlbumID, &album.AlbumName, &album.AlbumOwner, &album.OwnerFirst, &album.OwnerLast,
			&album.CreatedAt, &album.LockedAt, &album.UnlockedAt, &album.RevealedAt, &album.AlbumCoverID, &album.Visibility)
		if err != nil {
			log.Print(err)
		}
		QueryImagesData(ctx, connPool, &album, id)

		guestResponse, err := connPool.Pool.Query(ctx, guestQuery, album.AlbumID)
		if err != nil {
			log.Print(err)
		}

		guest := m.Guest{
			ID:        album.AlbumOwner,
			FirstName: album.OwnerFirst,
			LastName:  album.OwnerLast,
			Status:    "accepted",
		}
		guests = append(guests, guest)

		for guestResponse.Next() {
			err = guestResponse.Scan(&guest.ID, &guest.FirstName, &guest.LastName, &guest.Status)
			if err != nil {
				log.Print(err)
			}

			guests = append(guests, guest)
		}
		album.InviteList = guests

		err = album.PhaseCalculation()
		if err != nil {
			log.Print(err)
		}

		albums = append(albums, album)
	}

	var responseBytes []byte

	responseBytes, err = json.MarshalIndent(albums, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}

func GETAlbumsByUserID(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string, ctx context.Context) {
	var albums []m.Album

	albumQuery := `SELECT a.album_id, album_name, album_owner, u.first_name, u.last_name, a.created_at, locked_at, unlocked_at, revealed_at, album_cover_id, visibility
				   FROM albums a
				   JOIN albumuser au
				   ON au.album_id=a.album_id
				   JOIN users u
				   ON a.album_owner=u.user_id
				   WHERE au.user_id=(SELECT user_id FROM users WHERE auth_zero_id=$1)`

	guestQuery := `SELECT au.user_id, u.first_name, u.last_name, 'accepted' AS status
					FROM albumuser au
					JOIN users u ON u.user_id = au.user_id
					WHERE au.album_id = $1
					UNION
					SELECT u.user_id, u.first_name, u.last_name, ar.status
					FROM album_requests ar
					JOIN users u ON u.user_id = ar.invited_id
					WHERE ar.album_id = $1 AND ar.status IN ('pending', 'denied')`

	response, err := connPool.Pool.Query(ctx, albumQuery, uid)
	if err != nil {
		log.Print(err)
	}

	for response.Next() {
		var album m.Album
		var guests []m.Guest

		// Create Album Object
		err := response.Scan(&album.AlbumID, &album.AlbumName, &album.AlbumOwner, &album.OwnerFirst, &album.OwnerLast,
			&album.CreatedAt, &album.LockedAt, &album.UnlockedAt, &album.RevealedAt, &album.AlbumCoverID, &album.Visibility)
		if err != nil {
			log.Print(err)
		}

		// Fetch Albums Images
		QueryImagesData(ctx, connPool, &album, uid)

		guestResponse, err := connPool.Pool.Query(ctx, guestQuery, album.AlbumID)
		if err != nil {
			log.Print(err)
		}
		//guest := m.Guest{
		//	ID:        album.AlbumOwner,
		//	FirstName: album.OwnerFirst,
		//	LastName:  album.OwnerLast,
		//	Status:    "accepted",
		//}
		//guests = append(guests, guest)

		for guestResponse.Next() {
			var guest m.Guest

			err := guestResponse.Scan(&guest.ID, &guest.FirstName, &guest.LastName, &guest.Status)
			if err != nil {
				log.Print(err)
			}

			guests = append(guests, guest)
			album.InviteList = guests

		}

		err = album.PhaseCalculation()

		albums = append(albums, album)
	}

	var responseBytes []byte

	responseBytes, err = json.MarshalIndent(albums, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}

func POSTNewAlbum(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, uid string, messagingClient *messaging.Client) {
	album := m.Album{}

	bytes, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not read the request body")
		log.Printf("Failed Reading Body: %v", err)
		return
	}

	err = json.Unmarshal(bytes, &album)
	if err != nil {
		WriteErrorToWriter(w, "Error: Invalid request body - could not be mapped to object")
		log.Printf("Failed Unmarshaling: %v", err)
		return
	}

	newImageQuery := `INSERT INTO images
					  (image_owner, caption, upload_type)
					  VALUES ((SELECT user_id FROM users WHERE auth_zero_id=$1), $2, 'album_cover') RETURNING image_id`

	err = connPool.Pool.QueryRow(ctx, newImageQuery, uid, album.AlbumName).Scan(&album.AlbumCoverID)
	if err != nil {
		WriteErrorToWriter(w, "Unable to create entry in image table for album cover")
		log.Printf("Unable to create entry in image table for album cover: %v", err)
		return
	}

	createAlbumQuery := `INSERT INTO albums
						  (album_name, album_owner, album_cover_id, locked_at, unlocked_at, revealed_at, visibility)
						  VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2), $3, $4, $5, $6, $7) RETURNING album_id, created_at, album_owner`

	err = connPool.Pool.QueryRow(ctx, createAlbumQuery,
		album.AlbumName, uid, album.AlbumCoverID, album.LockedAt,
		album.UnlockedAt, album.RevealedAt, album.Visibility).Scan(&album.AlbumID, &album.CreatedAt, &album.AlbumOwner)
	if err != nil {
		WriteErrorToWriter(w, "Unable to create entry in albums table for new album - transaction cancelled")
		log.Printf("Unable to create entry in albums table for new album: %v", err)
		return
	}

	updateAlbumUserQuery := `INSERT INTO albumuser
						(album_id, user_id)
						VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2))`

	_, err = connPool.Pool.Exec(ctx, updateAlbumUserQuery, album.AlbumID, uid)
	if err != nil {
		WriteErrorToWriter(w, "Unable to associate album owner to the new album")
		log.Printf("Unable to associate album owner to the new album: %v", err)
		return
	}

	getOwnerDetailsQuery := `SELECT first_name, last_name FROM users WHERE auth_zero_id=$1`
	err = connPool.Pool.QueryRow(ctx, getOwnerDetailsQuery, uid).Scan(&album.OwnerFirst, &album.OwnerLast)
	if err != nil {
		WriteErrorToWriter(w, "Unable to create entry in albums table for new album - transaction cancelled")
		log.Printf("Unable to create entry in albums table for new album: %v", err)
		return
	}

	err = SendAlbumRequests(ctx, &album, album.InviteList, rdb, connPool, messagingClient)
	if err != nil {
		log.Printf("Sending album requests failed with error: %v", err)
		return
	}

	insertResponse, err := json.MarshalIndent(album, "", "\t")
	if err != nil {
		log.Print(err)
		return
	}
	responseBytes := []byte(insertResponse)

	w.Header().Set("Content-Type", "application/json") // add content length number of bytes
	w.Write(responseBytes)
}

func GETAlbumGuests(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context) {
	albumID := r.URL.Query().Get("album_id")
	var guests []m.Guest

	guestQuery := `SELECT au.user_id, u.first_name, u.last_name, 'accepted' AS status
							FROM albumuser au
							JOIN users u ON u.user_id = au.user_id
							WHERE au.album_id = $1
							UNION
							SELECT u.user_id, u.first_name, u.last_name, ar.status
							FROM album_requests ar
							JOIN users u ON u.user_id = ar.invited_id
							WHERE ar.album_id = $1 AND ar.status IN ('pending', 'denied')`

	rows, err := connPool.Pool.Query(ctx, guestQuery, albumID)
	if err != nil {
		log.Print(err)
		return
	}
	for rows.Next() {
		var guest m.Guest

		err = rows.Scan(&guest.ID, &guest.FirstName, &guest.LastName, &guest.Status)
		if err != nil {
			log.Printf("Failed guests: %v", err)
			return
		}

		guests = append(guests, guest)
	}

	insertResponse, err := json.MarshalIndent(guests, "", "\t")
	if err != nil {
		log.Print(err)
		return
	}
	responseBytes := []byte(insertResponse)

	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
	w.Write(responseBytes)

}

func PATCHAlbumVisibility(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool) {
	visibility := r.URL.Query().Get("visibility")
	albumID := r.URL.Query().Get("album_id")
	w.Header().Set("Content-Type", "application/json")

	updateQuery := `UPDATE albums
					SET visibility = $1
					WHERE album_id = $2`

	rowsEdited, err := connPool.Pool.Exec(ctx, updateQuery, visibility, albumID)
	if err != nil {
		log.Print(err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	if rowsEdited.RowsAffected() == 0 {
		response := "Album visibility not changed - no albums updated"
		log.Print(response)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(response))
		return
	}

	w.Write([]byte("Album visibility updated"))
}

func WriteErrorToWriter(w http.ResponseWriter, errorString string) {
	jsonString, err := json.MarshalIndent(errorString, "", "\t")
	if err != nil {
		log.Print(err)
		return
	}

	responseBytes := []byte(jsonString)

	w.Header().Set("Content-Type", "application/json") // add content length number of bytes
	w.Write(responseBytes)
}

func SendAlbumRequests(ctx context.Context, album *m.Album, invited []m.Guest, rdb *redis.Client, connPool *m.PGPool, messagingClient *messaging.Client) error {
	query := `INSERT INTO album_requests (album_id, invited_id) 
										VALUES ($1, $2) RETURNING request_id, updated_at, invite_seen, response_seen, status`
	var albumRequest = m.AlbumRequestNotification{
		AlbumID:      album.AlbumID,
		AlbumName:    album.AlbumName,
		AlbumCoverID: album.AlbumCoverID,
		AlbumOwner:   album.AlbumOwner,
		OwnerFirst:   album.OwnerFirst,
		OwnerLast:    album.OwnerLast,
		UnlockedAt:   album.UnlockedAt,
	}

	var fcmNotification = m.FirebaseNotification{
		NotificationID: album.AlbumID,
		ContentName:    album.AlbumName,
		RequesterID:    album.AlbumOwner,
		RequesterName:  album.OwnerFirst,
		Type:           "album-invite",
	}

	for _, user := range invited {
		var wsPayload WebSocketPayload

		albumRequest.GuestID = user.ID
		albumRequest.GuestFirst = user.FirstName
		albumRequest.GuestLast = user.LastName

		err := connPool.Pool.QueryRow(ctx, query, album.AlbumID, user.ID).Scan(&albumRequest.RequestID,
			&albumRequest.ReceivedAt, &albumRequest.InviteSeen, &albumRequest.ResponseSeen, &albumRequest.Status)
		if err != nil {
			log.Printf("Failed to add user to album request table: %v", err)
			return err
		}
		wsPayload.Operation = "REQUEST"
		wsPayload.Type = "album-invite"
		wsPayload.UserID = user.ID
		wsPayload.Payload = albumRequest

		jsonPayload, err := json.MarshalIndent(wsPayload, "", "\t")
		if err != nil {
			log.Print(err)
		}

		err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
		if err != nil {
			log.Print(err)
		}

		fcmNotification.RecipientID = user.ID

		err = SendFirebaseMessageToUID(ctx, connPool, messagingClient, fcmNotification)
		if err != nil {
			log.Print(err)
		}
	}

	return nil
}

func InviteUserToAlbum(ctx context.Context, w http.ResponseWriter, r *http.Request, rdb *redis.Client, connPool *m.PGPool, messagingClient *messaging.Client) {
	var albumRequest m.AlbumRequestNotification

	// Get Information from Request
	albumRequest.GuestID = r.URL.Query().Get("guest_id")
	albumRequest.AlbumID = r.URL.Query().Get("album_id")

	// Batch Request Query for Stored Information
	albumInfoRequestQuery := `SELECT album_name, album_cover_id, unlocked_at FROM albums WHERE album_id = $1`
	getGuestInfoQuery := `SELECT user_id, first_name, last_name FROM users WHERE auth_zero_id = $1`
	getRequesterInfoQuery := `SELECT first_name, last_name FROM users WHERE user_id = $1`

	// Create Album Request Query
	insertInviteQuery := `INSERT INTO album_requests (album_id, invited_id) 
										VALUES ($1, $2) RETURNING request_id, updated_at, invite_seen, response_seen, status`

	// Process Batch
	batch := &pgx.Batch{}
	batch.Queue(albumInfoRequestQuery, albumRequest.AlbumID)
	batch.Queue(getGuestInfoQuery, albumRequest.GuestID)
	batch.Queue(getRequesterInfoQuery, albumRequest.GuestID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)

	// Scan Batch Results
	err := batchResults.QueryRow().Scan(&albumRequest.AlbumName, &albumRequest.AlbumCoverID, &albumRequest.UnlockedAt)
	err = batchResults.QueryRow().Scan(&albumRequest.GuestID, &albumRequest.GuestFirst, &albumRequest.GuestLast)
	err = batchResults.QueryRow().Scan(&albumRequest.OwnerFirst, &albumRequest.OwnerFirst)
	if err != nil {
		log.Printf("Failure in batch: %v", err)
		responseBytes := []byte(err.Error())

		w.WriteHeader(401)
		w.Header().Set("Content-Type", "application/json") // add content length number of bytes
		w.Write(responseBytes)
	}

	// Execute Album Request Query
	err = connPool.Pool.QueryRow(ctx, insertInviteQuery, albumRequest.AlbumID, albumRequest.GuestID).Scan(&albumRequest.RequestID,
		&albumRequest.ReceivedAt, &albumRequest.InviteSeen, &albumRequest.ResponseSeen, &albumRequest.Status)
	if err != nil {
		log.Printf("Failed to add user to album request table: %v", err)
		responseBytes := []byte(err.Error())

		w.WriteHeader(401)
		w.Header().Set("Content-Type", "application/json") // add content length number of bytes
		w.Write(responseBytes)
	}

	// Assemble WS Payload Information
	var wsPayload WebSocketPayload
	wsPayload.Operation = "REQUEST"
	wsPayload.Type = "album-invite"
	wsPayload.UserID = albumRequest.GuestID
	wsPayload.Payload = albumRequest

	jsonPayload, err := json.MarshalIndent(wsPayload, "", "\t")
	if err != nil {
		log.Print(err)

		responseBytes := []byte(err.Error())

		w.WriteHeader(401)
		w.Header().Set("Content-Type", "application/json") // add content length number of bytes
		w.Write(responseBytes)
	}

	// Send to Notification Redis Channel
	err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
	if err != nil {
		log.Print(err)
		responseBytes := []byte(err.Error())

		w.WriteHeader(401)
		w.Header().Set("Content-Type", "application/json") // add content length number of bytes
		w.Write(responseBytes)
	}

	// Prepare Firebase Notification
	var fcmNotification = m.FirebaseNotification{
		RecipientID:    albumRequest.GuestID,
		NotificationID: albumRequest.AlbumID,
		ContentName:    albumRequest.AlbumName,
		RequesterID:    albumRequest.AlbumOwner,
		RequesterName:  albumRequest.OwnerFirst,
		Type:           "album-invite",
	}

	// Send Firebase Notification
	err = SendFirebaseMessageToUID(ctx, connPool, messagingClient, fcmNotification)
	if err != nil {
		log.Print(err)
		responseBytes := []byte(err.Error())

		w.WriteHeader(401)
		w.Header().Set("Content-Type", "application/json") // add content length number of bytes
		w.Write(responseBytes)
	}
}
