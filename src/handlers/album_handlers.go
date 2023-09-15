package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	m "last_weekend_services/src/models"

	"github.com/google/uuid"
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
}

func GETAlbumsByUID(w http.ResponseWriter, r *http.Request, connPool *m.PGPool) {
	albums := []Album{}

	uid, err := uuid.Parse(r.URL.Query().Get("uid"))
	if err != nil {
		var errorString string = fmt.Sprintln("Error: Provide a unique, valid UUID to return a user")
		responseBytes := []byte(errorString)

		w.Header().Set("Content-Type", "text/plain")
		w.Write(responseBytes)
		return
	}

	sqlQuery := `SELECT a.album_id, album_name, album_owner, created_at, locked_at, unlocked_at, revealed_at, album_cover_id
					FROM albums a
					JOIN albumuser au
					ON au.album_id=a.album_id
					WHERE au.user_id=$1`
	response, err := connPool.Pool.Query(context.Background(), sqlQuery, uid)
	if err != nil {
		log.Panic(err)
	}

	for response.Next() {
		var album Album
		err := response.Scan(&album.AlbumID, &album.AlbumName, &album.AlbumOwner,
			&album.CreatedAt, &album.LockedAt, &album.UnlockedAt, &album.RevealedAt, &album.AlbumCoverID)

		if err != nil {
			log.Panic(err)
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

func POSTNewAlbum(w http.ResponseWriter, r *http.Request, connPool *m.PGPool) {
	var album []Album

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

	sqlQuery := `INSERT INTO albums
				  (album_name, album_owner, album_cover_id, locked_at, unlocked_at, revealed_at)
				  VALUES ($1, $2, $3, $4, $5, $6) RETURNING album_id, created_at`
	err = connPool.Pool.QueryRow(context.Background(), sqlQuery,
		album[0].AlbumName, album[0].AlbumOwner, album[0].AlbumCoverID, album[0].LockedAt,
		album[0].UnlockedAt, album[0].RevealedAt).Scan(&album[0].AlbumID, &album[0].CreatedAt)
	if err != nil {
		log.Print(err)
		return
	}

	insertResponse, err := json.MarshalIndent(album, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}
	responseBytes := []byte(insertResponse)

	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
	w.Write(responseBytes)

}

func writeErrorToWriter(w http.ResponseWriter, errorString string) {
	jsonString, err := json.MarshalIndent(errorString, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	responseBytes := []byte(jsonString)

	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
	w.Write(responseBytes)
}
