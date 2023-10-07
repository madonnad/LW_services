package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	m "last_weekend_services/src/models"

	//jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

type Image struct {
	ID         string    `json:"image_id"`
	ImageOwner string    `json:"image_owner"`
	Caption    string    `json:"caption"`
	Upvotes    uint      `json:"upvotes"`
	CreatedAt  time.Time `json:"created_at"`
}

func ImageEndpointHandler(connPool *m.PGPool, rdb *redis.Client, ctx context.Context) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			GETImageFromID(w, r, connPool, ctx)
		case http.MethodPost:
			POSTNewImage(w, r, connPool, ctx)
		}
	})
}

func GETImagesFromUserID(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context) {
	images := []Image{}
	uid, err := uuid.Parse(r.URL.Query().Get("uid"))

	//ctxKey := jwtmiddleware.ContextKey{}

	//r.Context().Value(ctxKey)

	if err != nil {
		WriteErrorToWriter(w, "Error: Provide a unique, valid UUID to return a user's images")
		log.Print(err)
		return
	}

	query := `SELECT image_id, image_owner, caption, upvotes, created_at
			  FROM images
			  WHERE image_owner = $1`
	result, err := connPool.Pool.Query(ctx, query, uid)
	if err != nil {
		log.Print(err)
	}

	for result.Next() {
		var image Image
		err := result.Scan(&image.ID, &image.ImageOwner, &image.Caption, &image.Upvotes, &image.CreatedAt)
		if err != nil {
			log.Print(err)
		}

		images = append(images, image)
	}

	var responseBytes []byte
	if len(images) != 0 {
		responseBytes, err = json.MarshalIndent(images, "", "\t")
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

func GETImageFromID(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context) {
	image := Image{}

	uid, err := uuid.Parse(r.URL.Query().Get("uid"))
	if err != nil {
		WriteErrorToWriter(w, "Error: Provide a unique, valid UUID to return a image")
		log.Print(err)
		return
	}

	query := `SELECT image_id, image_owner, caption, upvotes, created_at
			  FROM images WHERE image_id = $1`

	results := connPool.Pool.QueryRow(ctx, query, uid)
	err = results.Scan(&image.ID, &image.ImageOwner, &image.Caption, &image.Upvotes, &image.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			WriteErrorToWriter(w, "Error: Image does not exist")
			log.Print("Error: Image does not exist")
			return
		} else {
			log.Print(err)
			return
		}
	}

	responseBytes, err := json.MarshalIndent(image, "", "\t")
	if err != nil {
		log.Print(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)

}

func POSTNewImage(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context) {
	image := Image{}

	bytes, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not read the request body")
		log.Print(err)
		return
	}

	err = json.Unmarshal(bytes, &image)
	if err != nil {
		WriteErrorToWriter(w, "Error: Invalid request body - could not be mapped to object")
		log.Print(err)
		return
	}

	query := `INSERT INTO images
			  (image_owner, caption) VALUES ($1, $2)
			  RETURNING image_id, created_at`
	err = connPool.Pool.QueryRow(ctx, query, image.ImageOwner, image.Caption).Scan(&image.ID, &image.CreatedAt)

	insertResponse, err := json.MarshalIndent(image, "", "\t")
	if err != nil {
		log.Print(err)
		return
	}
	responseBytes := []byte(insertResponse)

	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
	w.Write(responseBytes)
}

func QueryImagesData(ctx context.Context, connPool *m.PGPool, album *Album) {
	imageQuery := `SELECT i.image_id, image_owner, caption, upvotes, created_at
				   FROM images i
				   JOIN imagealbum ia
				   ON i.image_id=ia.image_id
				   WHERE ia.album_id=$1`

	images := []Image{}

	//Fetch Albums Images
	imageResponse, err := connPool.Pool.Query(ctx, imageQuery, album.AlbumID)
	if err != nil {
		log.Print(err)
	}

	for imageResponse.Next() {
		var image Image

		err := imageResponse.Scan(&image.ID, &image.ImageOwner, &image.Caption, &image.Upvotes, &image.CreatedAt)
		if err != nil {
			log.Print(err)
		}

		images = append(images, image)
	}
	album.Images = images
}
