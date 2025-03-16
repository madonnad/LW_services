package handlers

import (
	"cloud.google.com/go/storage"
	"context"
	"encoding/json"
	"errors"
	"firebase.google.com/go/v4/messaging"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	m "last_weekend_services/src/models"
	"log"
	"net/http"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/redis/go-redis/v9"
)

func AlbumEndpointHandler(connPool *m.PGPool, rdb *redis.Client, ctx context.Context, messagingClient *messaging.Client, gcpStorage storage.Client, liveBucket string) http.Handler {
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
				//case "/album/revealed":
				//	GETRevealedAlbumsByAlbumID(w, r, connPool, ctx)
				//case "/album/guests":
				//	GETAlbumGuests(w, r, connPool, ctx)
			}

		case http.MethodPost:
			switch r.URL.Path {
			case "/album":
				POSTNewAlbum(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject, messagingClient)
			case "/album/guests":
				InviteUserToAlbum(ctx, w, r, rdb, connPool, messagingClient)
			case "/album/revealed":
				GETRevealedAlbumsByAlbumID(w, r, connPool, ctx)
			}
		case http.MethodPatch:
			switch r.URL.Path {
			case "/user/album":
				PATCHAlbumOwner(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			case "/album/visibility":
				PATCHAlbumVisibility(ctx, w, r, connPool)
			}
		case http.MethodDelete:
			switch r.URL.Path {
			case "/album":
				DELETEAlbum(ctx, w, r, connPool, claims.RegisteredClaims.Subject, gcpStorage, liveBucket)
			case "/user/album":
				DELETEUserFromAlbum(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			}
		}

	})
}

func GETAlbumByAlbumID(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context, authZeroID string) {
	var hasAccess bool
	var album m.Album
	var guests []m.Guest
	batch := &pgx.Batch{}
	albumID := r.URL.Query().Get("album_id")

	accessQuery := `SELECT EXISTS (
						SELECT 1
						FROM albums a
						JOIN albumuser au ON a.album_id = au.album_id
						WHERE a.album_id = $1
						AND (
							a.visibility = 'public'
							OR (a.visibility = 'friends' AND EXISTS (
								SELECT 1
								FROM friends f
								WHERE (f.user1_id = au.user_id AND f.user2_id = (SELECT user_id FROM users WHERE auth_zero_id = $2))
								   OR (f.user2_id = au.user_id AND f.user1_id = (SELECT user_id FROM users WHERE auth_zero_id = $2))
							))
							OR (a.visibility = 'private' AND au.user_id = (SELECT user_id FROM users WHERE auth_zero_id = $2))
						)
					) AS has_access`

	err := connPool.Pool.QueryRow(ctx, accessQuery, albumID, authZeroID).Scan(&hasAccess)
	if err != nil {
		log.Printf("Error getting access to album: %v", err)
		WriteResponseWithCode(w, http.StatusNotFound, "Error getting access to album")
		return
	}

	if hasAccess == false {
		log.Printf("User does not have access to event")
		WriteResponseWithCode(w, http.StatusNotFound, "User does not have access to event")
		return
	}

	albumQuery := `SELECT a.album_id, album_name, album_owner, u.first_name, u.last_name, a.created_at, revealed_at, album_cover_id, visibility
					  FROM albums a
					  JOIN users u
					  ON a.album_owner=u.user_id
					  WHERE a.album_id=$1`

	guestQuery := `SELECT u.user_id, u.first_name, u.last_name, ar.status
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

	err = batchResults.QueryRow().Scan(&album.AlbumID, &album.AlbumName, &album.AlbumOwner, &album.OwnerFirst, &album.OwnerLast,
		&album.CreatedAt, &album.RevealedAt, &album.AlbumCoverID, &album.Visibility)
	if err != nil {

		if errors.Is(err, pgx.ErrNoRows) {
			WriteResponseWithCode(w, http.StatusNotFound, "Event Not Found")
			return
		}

		log.Print(err)
		return
	}

	QueryImagesData(ctx, connPool, &album, authZeroID)

	guestRows, err := batchResults.Query()
	if err != nil {
		log.Print(err)
	}

	// Does not require for the owner to be manually added since the owner is in album_request
	//guest := m.Guest{
	//	ID:        album.AlbumOwner,
	//	FirstName: album.OwnerFirst,
	//	LastName:  album.OwnerLast,
	//	Status:    "accepted",
	//}
	//guests = append(guests, guest)

	for guestRows.Next() {
		var guest m.Guest
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

	albumQuery := `SELECT a.album_id, album_name, album_owner, u.first_name, u.last_name, a.created_at, revealed_at, album_cover_id, visibility
					  FROM albums a
					  JOIN users u
					  ON a.album_owner=u.user_id
					  WHERE a.album_id=$1 AND a.revealed_at < CURRENT_DATE`

	guestQuery := `SELECT u.user_id, u.first_name, u.last_name, ar.status
					FROM users u
					JOIN album_requests ar
					ON u.user_id = ar.invited_id
					WHERE ar.album_id = $1`

	for _, id := range albumIDs {
		var album m.Album
		var guests []m.Guest

		err = connPool.Pool.QueryRow(ctx, albumQuery, id).Scan(&album.AlbumID, &album.AlbumName, &album.AlbumOwner, &album.OwnerFirst, &album.OwnerLast,
			&album.CreatedAt, &album.RevealedAt, &album.AlbumCoverID, &album.Visibility)
		if err != nil {
			log.Print(err)
		}
		QueryImagesData(ctx, connPool, &album, id)

		guestResponse, err := connPool.Pool.Query(ctx, guestQuery, id)
		if err != nil {
			log.Print(err)
		}

		// Deprecating because the owner is now being added into the original album_request query
		//guest := m.Guest{
		//	ID:        album.AlbumOwner,
		//	FirstName: album.OwnerFirst,
		//	LastName:  album.OwnerLast,
		//	Status:    "accepted",
		//}
		//guests = append(guests, guest)

		for guestResponse.Next() {

			var guest m.Guest
			err = guestResponse.Scan(&guest.ID, &guest.FirstName, &guest.LastName, &guest.Status)
			if err != nil {
				log.Print(err)
			}

			guests = append(guests, guest)
			album.InviteList = guests

			if id == "4ae4216a-5305-4d74-ba45-3af385a5d630" {
				log.Printf("Guest: %v", len(guests))
			}
		}

		err = album.PhaseCalculation()
		if err != nil {
			log.Print(err)
		}

		albums = append(albums, album)
		log.Printf("appended")
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

	albumQuery := `SELECT a.album_id, album_name, album_owner, u.first_name, u.last_name, a.created_at, revealed_at, album_cover_id, visibility
				   FROM albums a
				   JOIN albumuser au
				   ON au.album_id=a.album_id
				   JOIN users u
				   ON a.album_owner=u.user_id
				   WHERE au.user_id=(SELECT user_id FROM users WHERE auth_zero_id=$1)`

	// Original guest query before conversion to just album_requests
	//guestQuery := `SELECT au.user_id, u.first_name, u.last_name, 'accepted' AS status
	//				FROM albumuser au
	//				JOIN users u ON u.user_id = au.user_id
	//				WHERE au.album_id = $1
	//				UNION
	//				SELECT u.user_id, u.first_name, u.last_name, ar.status
	//				FROM album_requests ar
	//				JOIN users u ON u.user_id = ar.invited_id
	//				WHERE ar.album_id = $1 AND ar.status IN ('pending', 'denied')`

	// This query fetches all albums for a user and thus getting all guests within every album - may need to remove
	// this in the future since it can be fetched upon opening the album.
	guestQuery := `SELECT au.invited_id, u.first_name, u.last_name, au.status
					FROM album_requests au
					JOIN users u
					ON u.user_id = au.invited_id
					WHERE au.album_id = $1`

	response, err := connPool.Pool.Query(ctx, albumQuery, uid)
	if err != nil {
		log.Print(err)
	}

	for response.Next() {
		var album m.Album
		var guests []m.Guest

		// Create Album Object
		err := response.Scan(&album.AlbumID, &album.AlbumName, &album.AlbumOwner, &album.OwnerFirst, &album.OwnerLast,
			&album.CreatedAt, &album.RevealedAt, &album.AlbumCoverID, &album.Visibility)
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
		WriteResponseWithCode(w, http.StatusBadRequest, "Error: Could not read the request body")
		log.Printf("Failed Reading Body: %v", err)
		return
	}

	err = json.Unmarshal(bytes, &album)
	if err != nil {
		WriteResponseWithCode(w, http.StatusBadRequest, "Error: Invalid request body - could not be mapped to object")
		log.Printf("Failed Unmarshaling: %v", err)
		return
	}

	newImageQuery := `INSERT INTO images
					  (image_owner, caption, upload_type)
					  VALUES ((SELECT user_id FROM users WHERE auth_zero_id=$1), $2, 'album_cover') RETURNING image_id`

	err = connPool.Pool.QueryRow(ctx, newImageQuery, uid, album.AlbumName).Scan(&album.AlbumCoverID)
	if err != nil {
		WriteResponseWithCode(w, http.StatusInternalServerError, "Unable to create entry in image table for album cover")
		log.Printf("Unable to create entry in image table for album cover: %v", err)
		return
	}

	createAlbumQuery := `INSERT INTO albums
						  (album_name, album_owner, album_cover_id, revealed_at, visibility)
						  VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2), $3, $4, $5) RETURNING album_id, created_at, album_owner`

	err = connPool.Pool.QueryRow(ctx, createAlbumQuery,
		album.AlbumName, uid, album.AlbumCoverID, album.RevealedAt, album.Visibility).Scan(&album.AlbumID, &album.CreatedAt, &album.AlbumOwner)
	if err != nil {
		WriteResponseWithCode(w, http.StatusInternalServerError, "Unable to create entry in albums table for new album - transaction cancelled")
		log.Printf("Unable to create entry in albums table for new album: %v", err)
		return
	}

	albumRequestOwnerQuery := `INSERT INTO album_requests 
    							(album_id, invited_id, invite_seen, status, response_seen)
    							VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2), true, 'accepted', true)`

	_, err = connPool.Pool.Exec(ctx, albumRequestOwnerQuery, album.AlbumID, uid)
	if err != nil {
		WriteResponseWithCode(w, http.StatusInternalServerError, "Unable to add owner album request entry to new album")
		log.Printf("Unable to add owner album request entry to new album: %v", err)
		return
	}

	updateAlbumUserQuery := `INSERT INTO albumuser
						(album_id, user_id)
						VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2))`

	_, err = connPool.Pool.Exec(ctx, updateAlbumUserQuery, album.AlbumID, uid)
	if err != nil {
		WriteResponseWithCode(w, http.StatusInternalServerError, "Unable to associate album owner to the new album")
		log.Printf("Unable to associate album owner to the new album: %v", err)
		return
	}

	getOwnerDetailsQuery := `SELECT first_name, last_name FROM users WHERE auth_zero_id=$1`
	err = connPool.Pool.QueryRow(ctx, getOwnerDetailsQuery, uid).Scan(&album.OwnerFirst, &album.OwnerLast)
	if err != nil {
		WriteResponseWithCode(w, http.StatusInternalServerError, "Unable to create entry in albums table for new album - transaction cancelled")
		log.Printf("Unable to create entry in albums table for new album: %v", err)
		return
	}

	err = album.PhaseCalculation()
	if err != nil {
		WriteResponseWithCode(w, http.StatusInternalServerError, "Unable to calculate phase")
		log.Printf("Unable to calculate phase %v", err)
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

//func GETAlbumGuests(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context) {
//	albumID := r.URL.Query().Get("album_id")
//	var guests []m.Guest
//
//	guestQuery := `SELECT au.user_id, u.first_name, u.last_name, 'accepted' AS status
//							FROM albumuser au
//							JOIN users u ON u.user_id = au.user_id
//							WHERE au.album_id = $1
//							UNION
//							SELECT u.user_id, u.first_name, u.last_name, ar.status
//							FROM album_requests ar
//							JOIN users u ON u.user_id = ar.invited_id
//							WHERE ar.album_id = $1 AND ar.status IN ('pending', 'denied')`
//
//	rows, err := connPool.Pool.Query(ctx, guestQuery, albumID)
//	if err != nil {
//		log.Print(err)
//		return
//	}
//	for rows.Next() {
//		var guest m.Guest
//
//		err = rows.Scan(&guest.ID, &guest.FirstName, &guest.LastName, &guest.Status)
//		if err != nil {
//			log.Printf("Failed guests: %v", err)
//			return
//		}
//
//		guests = append(guests, guest)
//	}
//
//	insertResponse, err := json.MarshalIndent(guests, "", "\t")
//	if err != nil {
//		log.Print(err)
//		return
//	}
//	responseBytes := []byte(insertResponse)
//
//	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
//	w.Write(responseBytes)
//
//}

func PATCHAlbumOwner(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, authZeroID string) {
	var isOwner bool
	uid := r.URL.Query().Get("user_id")
	if uid == "" {
		log.Print("New owner ID not provided")
		WriteResponseWithCode(w, http.StatusNotFound, "New owner ID not provided")
		return
	}
	albumID := r.URL.Query().Get("album_id")
	if albumID == "" {
		log.Print("New event ID not provided")
		WriteResponseWithCode(w, http.StatusNotFound, "Event ID not provided")
		return
	}

	ownerQuery := `SELECT EXISTS(SELECT 1 FROM albums 
                    WHERE album_id = $1 
                    AND album_owner = (SELECT user_id FROM users WHERE auth_zero_id = $2))`
	query := `UPDATE albums 
				SET album_owner = $1 
				WHERE album_id = $2`

	rows, err := connPool.Pool.Query(ctx, ownerQuery, albumID, authZeroID)
	if err != nil {
		log.Print("Error querying album owner")
		WriteResponseWithCode(w, http.StatusBadRequest, "Error querying album owner")
		return
	}

	for rows.Next() {
		err = rows.Scan(&isOwner)
		if err != nil {
			log.Print("Error validating album owner")
			WriteResponseWithCode(w, http.StatusBadRequest, "Error validating album owner")
			return
		}
	}

	if isOwner == false {
		log.Print("Requester not current album owner")
		WriteResponseWithCode(w, http.StatusBadRequest, "Requester not current album owner")
		return
	}

	updatedRows, err := connPool.Pool.Exec(ctx, query, uid, albumID)
	if err != nil {
		log.Print("Error updating event information")
		WriteResponseWithCode(w, http.StatusBadRequest, "Error updating event information")
		return
	}

	if updatedRows.RowsAffected() == 0 {
		log.Print("Event was not updated")
		WriteResponseWithCode(w, http.StatusBadRequest, "Event was not updated")
		return
	}

	WriteResponseWithCode(w, http.StatusOK, "Event owner updated")
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

func DELETEAlbum(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string, gcpStorage storage.Client, bucket string) {
	var isOwner bool
	var images []string
	albumID := r.URL.Query().Get("album_id")
	if albumID == "" {
		log.Print("New event ID not provided")
		WriteResponseWithCode(w, http.StatusNotFound, "Event ID not provided")
		return
	}

	// i. Check that the user is the owner
	ownerQuery := `SELECT EXISTS(SELECT 1 FROM albums 
                    WHERE album_id = $1 
                    AND album_owner = (SELECT user_id FROM users WHERE auth_zero_id = $2))`
	err := connPool.Pool.QueryRow(ctx, ownerQuery, albumID, uid).Scan(&isOwner)
	if err != nil {
		log.Print("Error querying album owner")
		WriteResponseWithCode(w, http.StatusBadRequest, "Error querying album owner")
		return
	}

	if isOwner == false {
		log.Print("Requester not current album owner")
		WriteResponseWithCode(w, http.StatusBadRequest, "Requester not current album owner")
		return
	}

	tx, err := connPool.Pool.Begin(ctx)
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		WriteResponseWithCode(w, http.StatusInternalServerError, "Error starting transaction")
		return
	}
	defer tx.Rollback(ctx)

	// Items that need to be removed

	// 1. Remove the requests from the album_request table
	arRemoveQuery := `DELETE FROM album_requests WHERE album_id = $1`
	_, err = tx.Exec(ctx, arRemoveQuery, albumID)
	if err != nil {
		log.Printf("Error executing album requests query: %v", err)
		WriteResponseWithCode(w, http.StatusBadRequest, "Error executing album requests query")
		return
	}

	// May not be necessary as it could be possible that there are no requests
	//if arRows.RowsAffected() == 0 {
	//	log.Printf("No album requests deleted: %v", err)
	//	WriteResponseWithCode(w, http.StatusBadRequest, "No album requests deleted")
	//	return
	//}

	// 2. Remove the entries from the albumuser table
	auRemoveQuery := `DELETE FROM albumuser WHERE album_id = $1`
	_, err = tx.Exec(ctx, auRemoveQuery, albumID)
	if err != nil {
		log.Printf("Error executing albumuser query: %v", err)
		WriteResponseWithCode(w, http.StatusBadRequest, "Error executing albumuser query")
		return
	}

	// 3. Remove the images related from the images
	imageQuery := `DELETE FROM images i
					USING imagealbum ia
					WHERE ia.album_id = $1
					AND i.image_id = ia.image_id
					RETURNING i.image_id`
	imageIDs, err := tx.Query(ctx, imageQuery, albumID)
	if err != nil {
		log.Printf("Error deleting images: %v", err)
		WriteResponseWithCode(w, http.StatusBadRequest, "Error deleting images")
		return
	}
	for imageIDs.Next() {
		var image string
		err = imageIDs.Scan(&image)
		if err != nil {
			log.Printf("Error scanning imageID: %v", err)
			WriteResponseWithCode(w, http.StatusBadRequest, "Error scanning imageID")
			return
		}

		images = append(images, image)
	}

	//4. Remove the images from related from the imagealbum table
	//iaQuery := `DELETE FROM imagealbum WHERE album_id = $1`
	//_, err = tx.Exec(ctx, iaQuery, albumID)
	//if err != nil {
	//	log.Printf("Error executing imagealbum query: %v for album: %v", err, albumID)
	//	WriteResponseWithCode(w, http.StatusBadRequest, "Error executing imagealbum query")
	//	return
	//}

	// 5. Remove the album from the albums table - primary key cannot be violated
	var albumCoverID string
	albumDeleteQuery := `DELETE FROM albums WHERE album_id = $1 RETURNING album_cover_id`
	err = tx.QueryRow(ctx, albumDeleteQuery, albumID).Scan(&albumCoverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Printf("Could not query album cover ID: %v", err)
		} else {
			log.Printf("Error executing event delete: %v", err)
			WriteResponseWithCode(w, http.StatusBadRequest, "Error executing event delete")
			return
		}
	}

	images = append(images, albumCoverID)

	// 6. Remove the image data from the cloud database (could be down through a unique function - but may need to rely
	// on the success of the transaction first).
	for _, image := range images {
		smallImageID := fmt.Sprintf("%s_%d", image, 540)
		largeImageID := fmt.Sprintf("%s_%d", image, 1080)

		err = gcpStorage.Bucket(bucket).Object(image).Delete(ctx)
		if err != nil {
			log.Println(err)
		}
		err = gcpStorage.Bucket(bucket).Object(smallImageID).Delete(ctx)
		if err != nil {
			log.Println(err)
		}
		err = gcpStorage.Bucket(bucket).Object(largeImageID).Delete(ctx)
		if err != nil {
			log.Println(err)
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		log.Printf("Error commit transaction to delete the event: %v", err)
		WriteResponseWithCode(w, http.StatusInternalServerError, "Error commit transaction to delete the event")
		return
	}
	WriteResponseWithCode(w, http.StatusOK, "Success deleting event")
}

func DELETEUserFromAlbum(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	//TODO: Update this function so that the abandon query does not remove the owner from the album - but reassigns
	// then sets their album_request status to abandoned.
	albumID := r.URL.Query().Get("album_id")
	if albumID == "" {
		log.Print("New event ID not provided")
		WriteResponseWithCode(w, http.StatusNotFound, "Event ID not provided")
		return
	}

	auLeaveQuery := `DELETE FROM albumuser 
       				  WHERE album_id = $1 
       				  AND user_id = (SELECT user_id FROM users WHERE auth_zero_id = $2)`

	arUpdateQuery := `UPDATE album_requests ar
					SET status = 'abandoned', updated_at = now() AT TIME ZONE 'utc'
					WHERE album_id = $1
					AND invited_id = (SELECT user_id FROM users WHERE auth_zero_id = $2)`

	abandonQuery := `UPDATE images i
					SET abandoned = TRUE
					FROM imagealbum ia
					WHERE i.image_id = ia.image_id
					AND ia.album_id = $1
					AND i.image_owner = (SELECT user_id FROM users WHERE auth_zero_id = $2)`

	deletedRows, err := connPool.Pool.Exec(ctx, auLeaveQuery, albumID, uid)
	if err != nil {
		log.Printf("Error deleting user from event: %v", err)
		WriteResponseWithCode(w, http.StatusBadRequest, "Error deleting user from event")
		return
	}

	if deletedRows.RowsAffected() == 0 {
		log.Print("User was not deleted from the event")
		WriteResponseWithCode(w, http.StatusBadRequest, "User was not deleted from the event")
		return
	}

	updatedRows, err := connPool.Pool.Exec(ctx, arUpdateQuery, albumID, uid)
	if err != nil {
		log.Printf("Error updating the user album request: %v", err)
		WriteResponseWithCode(w, http.StatusBadRequest, "Error deleting user album request")
		return
	}

	if updatedRows.RowsAffected() == 0 {
		log.Print("Album request table was not updated")
		WriteResponseWithCode(w, http.StatusBadRequest, "Album request table was not updated")
		return
	}

	abandonedRows, err := connPool.Pool.Exec(ctx, abandonQuery, albumID, uid)
	if err != nil {
		log.Printf("Error deleting user from event: %v", err)
		WriteResponseWithCode(w, http.StatusBadRequest, "Error deleting user from event")
		return
	}

	if abandonedRows.RowsAffected() == 0 {
		log.Print("User was not deleted from the event")
		WriteResponseWithCode(w, http.StatusBadRequest, "User was not deleted from the event")
		return
	}

	WriteResponseWithCode(w, http.StatusOK, "User removed from event.")
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

func WriteResponseWithCode(w http.ResponseWriter, code int, message string) {
	jsonString, err := json.MarshalIndent(message, "", "\t")
	if err != nil {
		log.Print(err)
		return
	}

	responseBytes := []byte(jsonString)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
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
		RevealedAt:   album.RevealedAt,
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
		//fcmNotification.Payload = albumRequest

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
	albumInfoRequestQuery := `SELECT album_name, album_cover_id, revealed_at FROM albums WHERE album_id = $1`
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
	err := batchResults.QueryRow().Scan(&albumRequest.AlbumName, &albumRequest.AlbumCoverID, &albumRequest.RevealedAt)
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
