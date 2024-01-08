package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	m "last_weekend_services/src/models"
	"log"
	"net/http"
	"sort"

	//jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

func ImageEndpointHandler(connPool *m.PGPool, rdb *redis.Client, ctx context.Context) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			fmt.Fprintf(w, "Failed to get validated claims")
			return
		}
		switch r.Method {
		case http.MethodDelete:
			DELETEImageComment(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
		case http.MethodPatch:
			PATCHImageComment(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
		case http.MethodGet:
			switch r.URL.Path {
			case "/image/comment":
				GETImageComments(ctx, w, r, connPool)
			case "/user/image":
				GETImagesFromUserID(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			case "/user/album/image":
				GETImageFromID(ctx, w, r, connPool)
			}
		case http.MethodPost:
			switch r.URL.Path {
			case "/image/comment":
				POSTNewComment(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			case "/user/image":
				POSTNewImage(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			case "/user/recap":
				POSTImageToRecap(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			}
		}
	})
}

func DELETEImageComment(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	commentId, err := uuid.Parse(r.URL.Query().Get("id"))
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not fetch Comment ID")
		log.Printf("Could not fetch Comment ID: %v", err)
		return
	}

	query := `DELETE FROM comments
			  WHERE id=$1
			  AND user_id=(SELECT user_id FROM users WHERE auth_zero_id=$2)`

	status, err := connPool.Pool.Exec(ctx, query, commentId, uid)
	if err != nil {
		WriteErrorToWriter(w, "Error: Comment could not be deleted")
		log.Printf("Comment could not be deleted: %v", err)
		return
	}

	if status.RowsAffected() < 1 {
		WriteErrorToWriter(w, "Error: Return SQL status is not delete")
		log.Printf("Return SQL status is not delete %v", err)
		return
	}

	responseJSON, err := json.Marshal(true)
	if err != nil {
		http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)
}

func PATCHImageComment(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	var comment m.UpdateComment
	err := json.NewDecoder(r.Body).Decode(&comment)
	if err != nil {
		WriteErrorToWriter(w, "Error: Bad Comment")
		log.Printf("Unable to decode new comment: %v", err)
		return
	}

	query := `UPDATE comments
			  SET comment_text=$1, updated_at=(now() AT TIME ZONE 'utc'::text)
              WHERE id=$2 AND user_id=(SELECT user_id FROM users WHERE auth_zero_id=$3)`

	status, err := connPool.Pool.Exec(ctx, query, comment.Comment, comment.ID, uid)
	if err != nil {
		WriteErrorToWriter(w, "Error: Comment could not be updated")
		log.Printf("Comment could not be updated: %v", err)
		return
	}

	if status.RowsAffected() < 1 {
		WriteErrorToWriter(w, "Error: Return SQL status is not update")
		log.Printf("Return SQL status is not update %v", err)
		return
	}

	responseJSON, err := json.Marshal(status.Update())
	if err != nil {
		http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)
}

func GETImageComments(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool) {
	var comments []m.Comment
	imageId, err := uuid.Parse(r.URL.Query().Get("image_id"))
	if err != nil {
		WriteErrorToWriter(w, "Error: Couldn't get image id from request")
		log.Printf("Couldn't get image id from request: %v", err)
		return
	}

	query := `SELECT c.id, c.image_id, u.user_id, u.first_name, u.last_name ,c.comment_text, c.created_at, c.updated_at
				FROM comments c
				JOIN  users u
				ON u.user_id = c.user_id
				WHERE image_id=$1`

	result, err := connPool.Pool.Query(ctx, query, imageId)
	if err != nil {
		WriteErrorToWriter(w, "Error: Unable to query comments from DB")
		log.Printf("Unable to query comments from DB: %v", err)
		return
	}

	for result.Next() {
		var comment m.Comment
		err := result.Scan(&comment.ID, &comment.ImageID, &comment.UserID, &comment.FirstName, &comment.LastName, &comment.Comment, &comment.CreatedAt)
		if err != nil {
			WriteErrorToWriter(w, "Error: Failed to unpack response from DB")
			log.Printf("Failed to unpack response from DB: %v", err)
			return
		}

		comments = append(comments, comment)
	}

	sort.Slice(comments, func(i, j int) bool {
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})

	var responseBytes []byte
	responseBytes, err = json.MarshalIndent(comments, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}

func POSTNewComment(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	var newComment m.NewComment
	var newUid string

	err := json.NewDecoder(r.Body).Decode(&newComment)
	if err != nil {
		WriteErrorToWriter(w, "Error: Bad Comment")
		log.Printf("Unable to decode new comment: %v", err)
		return
	}

	query := `INSERT INTO comments (comment_text, image_id, user_id)
			  VALUES ($1, $2, (SELECT user_id FROM users WHERE auth_zero_id=$3)) RETURNING id`

	result := connPool.Pool.QueryRow(ctx, query, newComment.Comment, newComment.ImageID, uid)

	err = result.Scan(&newUid)
	if err != nil {
		WriteErrorToWriter(w, "Error: Couldn't post comment")
		log.Printf("Couldn't post comment: %v", err)
		return
	}

	responseJSON, err := json.Marshal(newUid)
	if err != nil {
		http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)
}

func GETImagesFromUserID(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	var images []m.Image

	query := `
			SELECT image_id, image_owner, caption, upvotes, created_at
			FROM images
			WHERE image_owner = (SELECT user_id FROM users WHERE auth_zero_id=$1);`
	result, err := connPool.Pool.Query(ctx, query, uid)
	if err != nil {
		log.Print(err)
	}

	for result.Next() {
		var image m.Image
		err := result.Scan(&image.ID, &image.ImageOwner, &image.Caption, &image.Upvotes, &image.CreatedAt)
		if err != nil {
			log.Print(err)
		}

		images = append(images, image)
	}

	var responseBytes []byte

	responseBytes, err = json.MarshalIndent(images, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)

}

func GETImageFromID(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool) {
	image := m.Image{}

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

func POSTImageToRecap(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	imageId := r.URL.Query().Get("id")

	query := `INSERT INTO imagerecap (recap_id, image_id) 
              VALUES ((SELECT recap_storage_id FROM recap_storage WHERE user_id = (SELECT user_id FROM users WHERE auth_zero_id=$1)), $2)`

	_, err := connPool.Pool.Exec(ctx, query, uid, imageId)
	if err != nil {
		WriteErrorToWriter(w, "Unable to add image to recap list")
		log.Printf("Unable to add image to recap list: %v", err)
		return
	}

	responseBytes := []byte("Success")

	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
	w.Write(responseBytes)
}

func POSTNewImage(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	// Add image to an album - album needs to be added in the body
	image := m.Image{}
	var album_id string
	var result map[string]interface{}

	image.ImageOwner = uid

	bytes, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not read the request body")
		log.Print(err)
		return
	}

	err = json.Unmarshal(bytes, &result)
	if err != nil {
		WriteErrorToWriter(w, "Error: Invalid request body - could not be mapped to object")
		log.Print(err)
		return
	}

	for key, value := range result {
		switch key {
		case "caption":
			if caption, ok := value.(string); ok {
				image.Caption = caption
			} else {
				fmt.Println("Value is not a string")
			}
		case "album_id":
			if id, ok := value.(string); ok {
				album_id = id
			} else {
				fmt.Println("Value is not a string")
			}
		}
	}

	imageCreationQuery := `INSERT INTO images
			  (image_owner, caption) VALUES ((SELECT user_id FROM users WHERE auth_zero_id=$1), $2)
			  RETURNING image_id, created_at`
	err = connPool.Pool.QueryRow(ctx, imageCreationQuery, image.ImageOwner, image.Caption).Scan(&image.ID, &image.CreatedAt)
	if err != nil {
		WriteErrorToWriter(w, "Unable to create image in database")
		log.Printf("Unable to create image in database: %v", err)
		return
	}

	addImageAlbum := `INSERT INTO imagealbum
					(image_id, album_id) VALUES ($1, $2)`
	_, err = connPool.Pool.Exec(ctx, addImageAlbum, image.ID, album_id)
	if err != nil {
		WriteErrorToWriter(w, "Unable to associate image to album")
		log.Printf("Unable to associate image to album: %v", err)
		return
	}

	insertResponse, err := json.MarshalIndent(image, "", "\t")
	if err != nil {
		log.Print(err)
		return
	}
	responseBytes := []byte(insertResponse)

	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
	w.Write(responseBytes)
}

func QueryImagesData(ctx context.Context, connPool *m.PGPool, album *m.Album) {
	imageQuery := `SELECT i.image_id, i.image_owner, u.first_name, u.last_name, i.caption, i.upvotes, i.created_at
				   FROM images i
				   JOIN imagealbum ia
				   ON i.image_id=ia.image_id
				   JOIN users u
				   ON i.image_owner=u.user_id
				   WHERE ia.album_id=$1`

	images := []m.Image{}

	//Fetch Albums Images
	imageResponse, err := connPool.Pool.Query(ctx, imageQuery, album.AlbumID)
	if err != nil {
		log.Print(err)
	}

	for imageResponse.Next() {
		var image m.Image

		err := imageResponse.Scan(&image.ID, &image.ImageOwner, &image.FirstName, &image.LastName, &image.Caption, &image.Upvotes, &image.CreatedAt)
		if err != nil {
			log.Print(err)
		}

		images = append(images, image)
	}
	album.Images = images
}
