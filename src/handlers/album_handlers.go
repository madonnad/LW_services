package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	m "last_weekend_services/src/models"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Album struct {
	AlbumID      string    `json:"album_id"`
	AlbumName    string    `json:"album_name"`
	AlbumOwner   string    `json:"album_owner"`
	AlbumCoverID string    `json:"album_cover_id"`
	CreatedAt    time.Time `json:"created_at"`
	LockedAt     time.Time `json:"locked_at"`
	UnlockedAt   time.Time `json:"unlocked_at"`
	RevealedAt   time.Time `json:"revealed_at"`
	InvitedList  []string  `json:"invited_list"`
}

func AlbumEndpointHandler(connPool *m.PGPool, rdb *redis.Client, ctx context.Context) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			GETAlbumsByUID(w, r, connPool)
		case http.MethodPost:
			POSTNewAlbum(ctx, w, r, connPool, rdb)
		}
	})
}

func GETAlbumsByUID(w http.ResponseWriter, r *http.Request, connPool *m.PGPool) {
	albums := []Album{}

	uid, err := uuid.Parse(r.URL.Query().Get("uid"))
	if err != nil {
		writeErrorToWriter(w, "Error: Provide a unique, valid UUID to return a user")

		return
	}

	query := `SELECT a.album_id, album_name, album_owner, created_at, locked_at, unlocked_at, revealed_at, album_cover_id
				 FROM albums a
				 JOIN albumuser au
				 ON au.album_id=a.album_id
				 WHERE au.user_id=$1`
	response, err := connPool.Pool.Query(context.Background(), query, uid)
	if err != nil {
		log.Print(err)
	}

	for response.Next() {
		var album Album
		err := response.Scan(&album.AlbumID, &album.AlbumName, &album.AlbumOwner,
			&album.CreatedAt, &album.LockedAt, &album.UnlockedAt, &album.RevealedAt, &album.AlbumCoverID)

		if err != nil {
			log.Print(err)
		}
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

func POSTNewAlbum(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client) {
	album := Album{}

	bytes, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		writeErrorToWriter(w, "Error: Could not read the request body")
		log.Print(err)
		return
	}

	err = json.Unmarshal(bytes, &album)
	if err != nil {
		writeErrorToWriter(w, "Error: Invalid request body - could not be mapped to object")
		log.Print(err)
		return
	}

	query := `INSERT INTO albums
				  (album_name, album_owner, album_cover_id, locked_at, unlocked_at, revealed_at)
				  VALUES ($1, $2, $3, $4, $5, $6) RETURNING album_id, created_at`
	err = connPool.Pool.QueryRow(context.Background(), query,
		album.AlbumName, album.AlbumOwner, album.AlbumCoverID, album.LockedAt,
		album.UnlockedAt, album.RevealedAt).Scan(&album.AlbumID, &album.CreatedAt)
	if err != nil {
		log.Print(err)
		return
	}

	err = SendAlbumRequests(ctx, album.AlbumID, album.InvitedList, rdb, connPool)
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

func writeErrorToWriter(w http.ResponseWriter, errorString string) {
	jsonString, err := json.MarshalIndent(errorString, "", "\t")
	if err != nil {
		log.Print(err)
		return
	}

	responseBytes := []byte(jsonString)

	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
	w.Write(responseBytes)
}

func SendAlbumRequests(ctx context.Context, albumID string, invited []string, rdb *redis.Client, connPool *m.PGPool) error {
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
		wsPayload.UserID = user
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
