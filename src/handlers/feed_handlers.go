package handlers

import (
	"context"
	"encoding/json"
	m "last_weekend_services/src/models"
	"log"
	"net/http"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
)

func FeedEndpointHandler(ctx context.Context, connPool *m.PGPool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodGet:
			GETAppFeed(ctx, w, connPool, claims.RegisteredClaims.Subject)
		}
	})
}

func GETAppFeed(ctx context.Context, w http.ResponseWriter, connPool *m.PGPool, uid string) {
	// Get a list of all user's albums I am interested in
	// Join that list on the list of albums and users
	// Join that on the list of albums to return the albums
	//Friends List: UID -> albumuser UID: Album ID -> albums Album ID: *

	albums := []Album{}

	query :=
		`SELECT a.album_id, a.album_name, a.album_owner, a.created_at, a.locked_at, a.unlocked_at, a.revealed_at, a.album_cover_id, a.visibility
		FROM albums a
		JOIN albumuser au
		ON a.album_id = au.album_id
		JOIN (
			SELECT
				CASE
					WHEN user1_id = $1 THEN user2_id
					WHEN user2_id = $1 THEN user1_id
				END AS friend_id
			FROM friends) fl
		ON au.user_id = fl.friend_id
		WHERE a.visibility = 'public' OR a.visibility = 'friends'
		UNION DISTINCT
		SELECT a.album_id, a.album_name, a.album_owner, a.created_at, a.locked_at, a.unlocked_at, a.revealed_at, a.album_cover_id, a.visibility
		FROM albums a
		JOIN albumuser au
		ON a.album_id = au.album_id
		WHERE au.user_id = $1`

	response, err := connPool.Pool.Query(ctx, query, uid)
	if err != nil {
		WriteErrorToWriter(w, "Feed SQL Query Failed")
		log.Print("Feed SQL Query Failed")
	}

	for response.Next() {
		var album Album

		err := response.Scan(&album.AlbumID, &album.AlbumName, &album.AlbumOwner,
			&album.CreatedAt, &album.LockedAt, &album.UnlockedAt, &album.RevealedAt, &album.AlbumCoverID, &album.Visibility)
		if err != nil {
			WriteErrorToWriter(w, "Scanning SQL response failed")
			log.Printf("Scanning the response failed with: %v", err)
		}

		QueryImagesData(ctx, connPool, &album)

		albums = append(albums, album)
	}

	var responseBytes []byte
	if len(albums) != 0 {
		responseBytes, err = json.MarshalIndent(albums, "", "\t")
		if err != nil {
			log.Panic(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(responseBytes)
	} else {
		errorString, err := json.MarshalIndent("Error: No Albums Found", "", "\t")
		if err != nil {
			log.Panic(err)
			return
		}
		responseBytes := []byte(errorString)

		w.Header().Set("Content-Type", "application/json") //add content length number of bytes
		w.Write(responseBytes)
	}

}
