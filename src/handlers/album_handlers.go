package handlers

import (
	"context"
	"encoding/json"
	"io"
	m "last_weekend_services/src/models"
	"log"
	"net/http"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/redis/go-redis/v9"
)

func AlbumEndpointHandler(connPool *m.PGPool, rdb *redis.Client, ctx context.Context) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodGet:
			GETAlbumsByUID(w, r, connPool, claims.RegisteredClaims.Subject, ctx)
		case http.MethodPost:
			POSTNewAlbum(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
		}
	})
}

func GETAlbumsByUID(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string, ctx context.Context) {
	var albums []m.Album

	albumQuery := `SELECT a.album_id, album_name, album_owner ,created_at, locked_at, unlocked_at, revealed_at, album_cover_id, visibility
				   FROM albums a
				   JOIN albumuser au
				   ON au.album_id=a.album_id
				   WHERE au.user_id=(SELECT user_id FROM users WHERE auth_zero_id=$1)`
	imageQuery := `SELECT i.image_id, image_owner, caption, upvotes, created_at
				   FROM images i
				   JOIN imagealbum ia
				   ON i.image_id=ia.image_id
				   WHERE ia.album_id=$1`

	guestQuery := `SELECT u.user_id,  u.first_name, u.last_name, false as accepted
					FROM users u
					JOIN album_requests ar
					ON u.user_id = ar.invited_id
					WHERE ar.album_id = $1
					UNION
					SELECT u.user_id,  u.first_name, u.last_name, true as accepted
					FROM users u
					JOIN albumuser au
					ON u.user_id = au.user_id
					WHERE au.album_id = $1`

	response, err := connPool.Pool.Query(ctx, albumQuery, uid)
	if err != nil {
		log.Print(err)
	}

	for response.Next() {
		var album m.Album
		var images []m.Image
		var guests []m.Guest

		//Create Album Object
		err := response.Scan(&album.AlbumID, &album.AlbumName, &album.AlbumOwner,
			&album.CreatedAt, &album.LockedAt, &album.UnlockedAt, &album.RevealedAt, &album.AlbumCoverID, &album.Visibility)
		if err != nil {
			log.Print(err)
		}

		//Fetch Albums Images
		imageResponse, err := connPool.Pool.Query(ctx, imageQuery, album.AlbumID)
		if err != nil {
			log.Print(err)
		}

		for imageResponse.Next() {
			var image m.Image

			err := imageResponse.Scan(&image.ID, &image.ImageOwner, &image.Caption, &image.Upvotes, &image.CreatedAt)
			if err != nil {
				log.Print(err)
			}

			images = append(images, image)
			album.Images = images
		}

		guestResponse, err := connPool.Pool.Query(ctx, guestQuery, album.AlbumID)
		if err != nil {
			log.Print(err)
		}

		for guestResponse.Next() {
			var guest m.Guest

			err := guestResponse.Scan(&guest.ID, &guest.FirstName, &guest.LastName, &guest.Accepted)
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

func POSTNewAlbum(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, uid string) {
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
					  (image_owner, caption)
					  VALUES ($1, $2) RETURNING image_id`

	err = connPool.Pool.QueryRow(ctx, newImageQuery, uid, album.AlbumName).Scan(&album.AlbumCoverID)
	if err != nil {
		WriteErrorToWriter(w, "Unable to create entry in image table for album cover")
		log.Printf("Unable to create entry in image table for album cover: %v", err)
		return
	}

	createAlbumQuery := `INSERT INTO albums
						  (album_name, album_owner, album_cover_id, locked_at, unlocked_at, revealed_at)
						  VALUES ($1, (SELECT user_id FROM users WHERE auth_zero_id=$2), $3, $4, $5, $6) RETURNING album_id, created_at`

	err = connPool.Pool.QueryRow(ctx, createAlbumQuery,
		album.AlbumName, uid, album.AlbumCoverID, album.LockedAt,
		album.UnlockedAt, album.RevealedAt).Scan(&album.AlbumID, &album.CreatedAt)
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

	err = SendAlbumRequests(ctx, album.AlbumID, album.InviteList, rdb, connPool)
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

	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
	w.Write(responseBytes)

}

func WriteErrorToWriter(w http.ResponseWriter, errorString string) {
	jsonString, err := json.MarshalIndent(errorString, "", "\t")
	if err != nil {
		log.Print(err)
		return
	}

	responseBytes := []byte(jsonString)

	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
	w.Write(responseBytes)
}

func SendAlbumRequests(ctx context.Context, albumID string, invited []m.Guest, rdb *redis.Client, connPool *m.PGPool) error {
	query := `INSERT INTO album_requests (album_id, invited_id) VALUES ($1, $2) RETURNING invited_at`

	for _, user := range invited {
		var wsPayload WebSocketPayload
		result := connPool.Pool.QueryRow(ctx, query, albumID, user)
		err := result.Scan(&wsPayload.Received)
		if err != nil {
			log.Printf("Failed to add user to album request table: %v", err)
			return err
		}
		wsPayload.Operation = "INSERT"
		wsPayload.Type = "album_request"
		wsPayload.UserID = user.ID
		wsPayload.Payload = albumID

		jsonPayload, err := json.MarshalIndent(wsPayload, "", "\t")
		if err != nil {
			log.Print(err)
		}

		err = rdb.Publish(ctx, "album-requests", jsonPayload).Err()
		if err != nil {
			log.Print(err)
		}
	}

	return nil
}
