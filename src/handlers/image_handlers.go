package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	m "last_weekend_services/src/models"
	"log"
	"net/http"
	"sort"
	"time"

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
			switch r.URL.Path {
			case "/image/comment":
				DELETEImageComment(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			case "/image/like":
				DELETEImageLike(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
			case "/image/upvote":
				DELETEImageUpvote(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
			}
		case http.MethodPatch:
			switch r.URL.Path {
			case "/image/comment":
				PATCHImageComment(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			case "/image/comment/seen":
				PATCHCommentSeen(ctx, w, r, connPool)
			case "/user/image":
				PATCHUpdateImageAlbum(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			}

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
			case "/image/upvote":
				POSTImageUpvote(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
			case "/image/like":
				POSTImageLike(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
			case "/image/comment":
				POSTNewComment(ctx, w, r, connPool, rdb, claims.RegisteredClaims.Subject)
			case "/user/image":
				POSTNewImage(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			case "/user/recap":
				POSTImageToRecap(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
			}

		}
	})
}
func DELETEImageUpvote(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, uid string) {
	notification := m.EngagementNotification{
		NotificationType: `upvote`,
	}

	notification.ImageID = r.URL.Query().Get("image_id")
	var image_owner string

	imageOwnerQuery := `SELECT u.auth_zero_id, i.image_owner, a.album_id
						FROM images i
						JOIN users u
						ON i.image_owner = u.user_id 
						JOIN imagealbum ia
						ON i.image_id = ia.image_id
						JOIN albums a 
						ON a.album_id = ia.album_id
						WHERE i.image_id=$1`

	err := connPool.Pool.QueryRow(ctx, imageOwnerQuery, notification.ImageID).Scan(&image_owner, &notification.ReceiverID, &notification.AlbumID)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get image_owner")
		log.Printf("Could not get image_owner: %v", err)
		return
	}
	// Setup the notification in the event that the image_owner is unliking their own image
	notification.NotifierID = notification.ReceiverID

	upvoteQuery := `DELETE FROM upvotes
			  WHERE (image_id=$1
			  AND user_id=(SELECT user_id FROM users WHERE auth_zero_id=$2))`
	notificationQuery := `DELETE FROM notifications
						WHERE (media_id=$1
						AND sender_id=(SELECT user_id FROM users WHERE auth_zero_id=$2)
						AND type='upvote')
						RETURNING album_id, receiver_id, sender_id`

	batch := &pgx.Batch{}
	batch.Queue(upvoteQuery, &notification.ImageID, uid)
	if image_owner != uid {
		batch.Queue(notificationQuery, &notification.ImageID, uid)
	}
	batchResults := connPool.Pool.SendBatch(ctx, batch)
	defer func() {
		err := batchResults.Close()
		if err != nil {
			log.Printf("%v", err)
			return
		}
	}()

	status, err := batchResults.Exec()
	if err != nil {
		WriteErrorToWriter(w, "Error: Upvote could not be deleted")
		log.Printf("Upvote could not be deleted: %v", err)
		return
	}
	if status.RowsAffected() < 1 {
		WriteErrorToWriter(w, "Error: Return SQL status is not delete")
		log.Printf("Return SQL status is not delete %v", err)
		return
	}

	if image_owner != uid {
		err = batchResults.QueryRow().Scan(&notification.AlbumID, &notification.ReceiverID, &notification.NotifierID)
		if err != nil {
			WriteErrorToWriter(w, "Error: Notification could not be deleted")
			log.Printf("Notification could not be deleted: %v", err)
			return
		}
	}

	countQuery := `SELECT COUNT(*) FROM upvotes WHERE image_id=$1`

	countResponse := connPool.Pool.QueryRow(ctx, countQuery, &notification.ImageID)
	err = countResponse.Scan(&notification.NewCount)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get upvote count")
		log.Printf("Could not get upvote count: %v", err)
		return
	}

	payload := WebSocketPayload{
		Operation: `REMOVE`,
		Type:      `upvote`,
		UserID:    notification.ReceiverID,
		AlbumID:   notification.AlbumID,
		Payload:   notification,
	}
	payload.Payload = notification

	// Send payload to WebSocket
	jsonPayload, jsonErr := json.MarshalIndent(payload, "", "\t")
	if jsonErr != nil {
		log.Print(jsonErr)
	}

	err = rdb.Publish(ctx, notification.AlbumID, jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	responseJSON, err := json.MarshalIndent(notification, "", "\t")
	if err != nil {
		http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)
}

func POSTImageUpvote(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, uid string) {
	batch := &pgx.Batch{}
	notification := m.EngagementNotification{
		NotificationType: `upvote`,
	}

	imageID, err := uuid.Parse(r.URL.Query().Get("image_id"))
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not parse image id from request")
		log.Printf("Could not parse image id from request: %v", err)
		return
	}

	// TODO: Remove upvote_id from upvotes table
	addToUpvotesQuery := `INSERT INTO upvotes (user_id, image_id)
			  VALUES ((SELECT user_id FROM users WHERE auth_zero_id=$1), $2)
			  RETURNING user_id, image_id`

	// Batched Queries
	countQuery := `SELECT COUNT(*) FROM upvotes WHERE image_id=$1`
	albumDataQuery := `SELECT a.album_id, a.album_name, i.image_owner
						FROM albums a
						JOIN imagealbum ia
						ON a.album_id = ia.album_id
						JOIN images i 
						ON ia.image_id = i.image_id
						WHERE ia.image_id=$1`
	engagerQuery := `SELECT first_name, last_name FROM users WHERE user_id=$1`

	// Add Notification to be Sent to user
	addToNotifications := `INSERT INTO notifications (album_id, media_id, sender_id, receiver_id, type) 
							VALUES ($1, $2, $3, $4, 'upvote')
							RETURNING notification_uid, received_at, seen`

	// Add to Upvote Table Query
	err = connPool.Pool.QueryRow(ctx, addToUpvotesQuery, uid, imageID).Scan(&notification.NotifierID,
		&notification.ImageID)
	if err != nil {
		WriteErrorToWriter(w, "Error: Image could not be upvoted")
		log.Printf("Image could not be upvoted: %v", err)
		return
	}

	batch.Queue(countQuery, imageID)
	batch.Queue(albumDataQuery, imageID)
	batch.Queue(engagerQuery, &notification.NotifierID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)
	defer func() {
		err := batchResults.Close()
		if err != nil {
			log.Printf("%v", err)
			return
		}
	}()

	//Count Query
	err = batchResults.QueryRow().Scan(&notification.NewCount)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get upvote count")
		log.Printf("Could not get upvote count: %v", err)
		return
	}

	// Album Data Query
	err = batchResults.QueryRow().Scan(&notification.AlbumID, &notification.AlbumName, &notification.ReceiverID)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get upvote album data")
		log.Printf("Could not get upvote album data: %v", err)
		return
	}

	// Engager Query
	err = batchResults.QueryRow().Scan(&notification.NotifierFirst, &notification.NotifierLast)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get upvote engager data")
		log.Printf("Could not get upvote engager data: %v", err)
		return
	}

	// Add to Notification Table for Auth'd User
	if notification.NotifierID != notification.ReceiverID {
		err = connPool.Pool.QueryRow(ctx, addToNotifications, &notification.AlbumID, &notification.ImageID,
			&notification.NotifierID, notification.ReceiverID).Scan(&notification.NotificationID, &notification.ReceivedAt, &notification.NotificationSeen)
		if err != nil {
			WriteErrorToWriter(w, "Error: With adding to notification table")
			log.Printf("Adding to notification table error: %v", err)
			return
		}
	}

	payload := WebSocketPayload{
		Operation: `ADD`,
		Type:      `upvote`,
		UserID:    notification.ReceiverID,
		AlbumID:   notification.AlbumID,
		Payload:   notification,
	}
	payload.Payload = notification

	// Send payload to WebSocket
	jsonPayload, jsonErr := json.MarshalIndent(payload, "", "\t")
	if jsonErr != nil {
		log.Print(jsonErr)
	}

	err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	err = rdb.Publish(ctx, notification.AlbumID, jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	responseJSON, err := json.MarshalIndent(notification.NewCount, "", "\t")
	if err != nil {
		http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)
}

func DELETEImageLike(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, uid string) {
	notification := m.EngagementNotification{
		NotificationType: `liked`,
	}

	notification.ImageID = r.URL.Query().Get("image_id")
	var image_owner string

	imageOwnerQuery := `SELECT u.auth_zero_id, i.image_owner, a.album_id
						FROM images i
						JOIN users u
						ON i.image_owner = u.user_id 
						JOIN imagealbum ia
						ON i.image_id = ia.image_id
						JOIN albums a 
						ON a.album_id = ia.album_id
						WHERE i.image_id=$1`

	err := connPool.Pool.QueryRow(ctx, imageOwnerQuery, notification.ImageID).Scan(&image_owner, &notification.ReceiverID, &notification.AlbumID)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get image_owner")
		log.Printf("Could not get image_owner: %v", err)
		return
	}
	// Setup the notification in the event that the image_owner is unliking their own image
	notification.NotifierID = notification.ReceiverID

	upvoteQuery := `DELETE FROM likes
			  WHERE (image_id=$1
			  AND user_id=(SELECT user_id FROM users WHERE auth_zero_id=$2))`
	notificationQuery := `DELETE FROM notifications
						WHERE (media_id=$1
						AND sender_id=(SELECT user_id FROM users WHERE auth_zero_id=$2)
						AND type='liked')
						RETURNING album_id, receiver_id, sender_id`

	batch := &pgx.Batch{}
	batch.Queue(upvoteQuery, &notification.ImageID, uid)
	if image_owner != uid {
		batch.Queue(notificationQuery, &notification.ImageID, uid)
	}
	batchResults := connPool.Pool.SendBatch(ctx, batch)
	defer func() {
		err := batchResults.Close()
		if err != nil {
			log.Printf("%v", err)
			return
		}
	}()

	status, err := batchResults.Exec()
	if err != nil {
		WriteErrorToWriter(w, "Error: Like could not be deleted")
		log.Printf("Like could not be deleted: %v", err)
		return
	}
	if status.RowsAffected() < 1 {
		WriteErrorToWriter(w, "Error: Return SQL status is not delete")
		log.Printf("Return SQL status is not delete %v", err)
		return
	}

	if image_owner != uid {
		err = batchResults.QueryRow().Scan(&notification.AlbumID, &notification.ReceiverID, &notification.NotifierID)
		if err != nil {
			WriteErrorToWriter(w, "Error: Notification could not be deleted")
			log.Printf("Notification could not be deleted: %v", err)
			return
		}
	}

	countQuery := `SELECT COUNT(*) FROM likes WHERE image_id=$1`

	countResponse := connPool.Pool.QueryRow(ctx, countQuery, &notification.ImageID)
	err = countResponse.Scan(&notification.NewCount)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get like count")
		log.Printf("Could not get like count: %v", err)
		return
	}

	payload := WebSocketPayload{
		Operation: `REMOVE`,
		Type:      `liked`,
		UserID:    notification.ReceiverID,
		AlbumID:   notification.AlbumID,
		Payload:   notification,
	}
	payload.Payload = notification

	// Send payload to WebSocket
	jsonPayload, jsonErr := json.MarshalIndent(payload, "", "\t")
	if jsonErr != nil {
		log.Print(jsonErr)
	}

	err = rdb.Publish(ctx, notification.AlbumID, jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	responseJSON, err := json.MarshalIndent(notification, "", "\t")
	if err != nil {
		http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)
}

func POSTImageLike(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, uid string) {
	batch := &pgx.Batch{}
	notification := m.EngagementNotification{
		NotificationType: `liked`,
	}

	imageID, err := uuid.Parse(r.URL.Query().Get("image_id"))
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not parse image id from request")
		log.Printf("Could not parse image id from request: %v", err)
		return
	}

	// Add to Like Table Query
	addToLikesQuery := `INSERT INTO likes (user_id, image_id)
			  VALUES ((SELECT user_id FROM users WHERE auth_zero_id=$1), $2)
			  RETURNING user_id, image_id`

	// Batched Queries
	countQuery := `SELECT COUNT(*) FROM likes WHERE image_id=$1`
	albumDataQuery := `SELECT a.album_id, a.album_name, i.image_owner
						FROM albums a
						JOIN imagealbum ia
						ON a.album_id = ia.album_id
						JOIN images i 
						ON ia.image_id = i.image_id
						WHERE ia.image_id=$1`
	engagerQuery := `SELECT first_name, last_name FROM users WHERE user_id=$1`

	// Add Notification to be Sent to user
	addToNotifications := `INSERT INTO notifications (album_id, media_id, sender_id, receiver_id, type) 
							VALUES ($1, $2, $3, $4, 'liked')
							RETURNING notification_uid, received_at, seen`

	err = connPool.Pool.QueryRow(ctx, addToLikesQuery, uid, imageID).Scan(&notification.NotifierID,
		&notification.ImageID)
	if err != nil {
		WriteErrorToWriter(w, "Error: Image could not be liked")
		log.Printf("Image could not be liked: %v", err)
		return
	}

	batch.Queue(countQuery, imageID)
	batch.Queue(albumDataQuery, imageID)
	batch.Queue(engagerQuery, &notification.NotifierID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)
	defer func() {
		err := batchResults.Close()
		if err != nil {
			log.Printf("%v", err)
			return
		}
	}()

	err = batchResults.QueryRow().Scan(&notification.NewCount)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get like count")
		log.Printf("Could not get like count: %v", err)
		return
	}
	// Album Data Query
	err = batchResults.QueryRow().Scan(&notification.AlbumID, &notification.AlbumName, &notification.ReceiverID)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get liked image album data")
		log.Printf("Could not get liked image album data: %v", err)
		return
	}

	// Engager Query
	err = batchResults.QueryRow().Scan(&notification.NotifierFirst, &notification.NotifierLast)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get liked image engager data")
		log.Printf("Could not get liked image engager data: %v", err)
		return
	}

	// Add to Notification Table for Auth'd User
	if notification.NotifierID != notification.ReceiverID {
		err = connPool.Pool.QueryRow(ctx, addToNotifications, &notification.AlbumID, &notification.ImageID,
			&notification.NotifierID, notification.ReceiverID).Scan(&notification.NotificationID,
			&notification.ReceivedAt, &notification.NotificationSeen)
		if err != nil {
			WriteErrorToWriter(w, "Error: With adding to notification table")
			log.Printf("Adding to notification table error: %v", err)
			return
		}
	}

	payload := WebSocketPayload{
		Operation: `ADD`,
		Type:      `liked`,
		UserID:    notification.ReceiverID,
		AlbumID:   notification.AlbumID,
		Payload:   notification,
	}

	// Send payload to WebSocket
	jsonPayload, jsonErr := json.MarshalIndent(payload, "", "\t")
	if jsonErr != nil {
		log.Print(jsonErr)
	}

	err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	err = rdb.Publish(ctx, notification.AlbumID, jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	responseJSON, err := json.MarshalIndent(notification.NewCount, "", "\t")
	if err != nil {
		http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)
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
			  AND commenter_id=(SELECT user_id FROM users WHERE auth_zero_id=$2)`

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
              WHERE id=$2 AND commenter_id=(SELECT user_id FROM users WHERE auth_zero_id=$3)`

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

func PATCHCommentSeen(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool) {
	commentID := r.URL.Query().Get("id")

	seenQuery := `UPDATE comments SET seen = true WHERE id=$1`

	_, err := connPool.Pool.Exec(ctx, seenQuery, commentID)
	if err != nil {
		WriteErrorToWriter(w, "Error: Couldn't update comment to seen")
		log.Printf("Couldn't update comment to seen: %v", err)
		return
	}
	//Respond to the calling user that the action was successful
	responseBytes, err := json.MarshalIndent("Comment was marked as seen", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)

}

func GETImageComments(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool) {
	var comments []m.Comment
	imageId, err := uuid.Parse(r.URL.Query().Get("image_id"))
	if err != nil {
		WriteErrorToWriter(w, "Error: Couldn't get image id from request")
		log.Printf("Couldn't get image id from request: %v", err)
		return
	}

	query := `SELECT c.id, c.image_id, u.user_id, u.first_name, u.last_name ,c.comment_text, c.created_at, c.updated_at, c.seen
				FROM comments c
				JOIN  users u
				ON u.user_id = c.commenter_id
				WHERE image_id=$1`

	result, err := connPool.Pool.Query(ctx, query, imageId)
	if err != nil {
		WriteErrorToWriter(w, "Error: Unable to query comments from DB")
		log.Printf("Unable to query comments from DB: %v", err)
		return
	}

	for result.Next() {
		var comment m.Comment
		err := result.Scan(&comment.ID, &comment.ImageID, &comment.UserID, &comment.FirstName, &comment.LastName,
			&comment.Comment, &comment.CreatedAt, &comment.UpdatedAt, &comment.Seen)
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

func POSTNewComment(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, uid string) {
	var comment m.Comment

	err := json.NewDecoder(r.Body).Decode(&comment)
	if err != nil {
		WriteErrorToWriter(w, "Error: Bad Comment")
		log.Printf("Unable to decode new comment: %v", err)
		return
	}

	// Add the comment to the comment table
	addCommentQuery := `INSERT INTO comments (comment_text, image_id, commenter_id)
			  			VALUES ($1, $2, (SELECT user_id FROM users WHERE auth_zero_id=$3))
			  			RETURNING id, commenter_id, comment_text, created_at, seen`

	imageDataQuery := `SELECT image_owner, ia.album_id, a.album_name
						FROM images i
						JOIN imagealbum ia
						ON i.image_id = ia.image_id
						JOIN albums a 
						ON a.album_id = ia.album_id
						WHERE i.image_id = $1`

	commenterInfoQuery := `SELECT first_name, last_name FROM users WHERE user_id = $1`

	err = connPool.Pool.QueryRow(ctx, addCommentQuery, comment.Comment, comment.ImageID, uid).Scan(
		&comment.ID, &comment.UserID, &comment.Comment, &comment.CreatedAt, &comment.Seen)
	if err != nil {
		WriteErrorToWriter(w, "Error: Couldn't post comment")
		log.Printf("Couldn't post comment: %v", err)
		return
	}

	batch := &pgx.Batch{}
	batch.Queue(imageDataQuery, comment.ImageID)
	batch.Queue(commenterInfoQuery, comment.UserID)
	batchResults := connPool.Pool.SendBatch(ctx, batch)
	defer func() {
		err := batchResults.Close()
		if err != nil {
			log.Printf("%v", err)
			return
		}
	}()

	err = batchResults.QueryRow().Scan(&comment.ImageOwner, &comment.AlbumID, &comment.AlbumName)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get image owner")
		log.Printf("Could not get image owner: %v", err)
		return
	}
	err = batchResults.QueryRow().Scan(&comment.FirstName, &comment.LastName)
	if err != nil {
		WriteErrorToWriter(w, "Error: Could not get commenter information")
		log.Printf("Could not get commenter information: %v", err)
		return
	}

	if comment.ImageOwner == comment.UserID {
		markRead := `UPDATE comments SET seen = true WHERE id = $1`
		_, err = connPool.Pool.Exec(ctx, markRead, comment.ID)
		if err != nil {
			WriteErrorToWriter(w, "Error: Could not get mark comment as read")
			log.Printf("Could not get mark comment as read: %v", err)
			return
		}
	}

	payload := WebSocketPayload{
		Operation: `ADD`,
		Type:      `comment`,
		UserID:    comment.ImageOwner,
		AlbumID:   comment.AlbumID,
		Payload:   comment,
	}

	// Send payload to WebSocket
	jsonPayload, jsonErr := json.MarshalIndent(payload, "", "\t")
	if jsonErr != nil {
		log.Print(jsonErr)
	}

	err = rdb.Publish(ctx, "notifications", jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	err = rdb.Publish(ctx, payload.AlbumID, jsonPayload).Err()
	if err != nil {
		log.Print(err)
	}

	responseJSON, err := json.Marshal(comment)
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
			SELECT image_id, image_owner, caption, upload_type, created_at
			FROM images
			WHERE image_owner = (SELECT user_id FROM users WHERE auth_zero_id=$1);`
	result, err := connPool.Pool.Query(ctx, query, uid)
	if err != nil {
		log.Print(err)
	}

	for result.Next() {
		var image m.Image
		err := result.Scan(&image.ID, &image.ImageOwner, &image.Caption, &image.UploadType, &image.CapturedAt)
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
	err = results.Scan(&image.ID, &image.ImageOwner, &image.Caption, &image.Upvotes, &image.CapturedAt)
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
		case "uuid":
			if imageID, ok := value.(string); ok {
				image.ID = imageID
			} else {
				log.Println("Error: Image UUID not provided")
				WriteErrorToWriter(w, "Error: Image UUID not provided")
				return
			}
		case "caption":
			if caption, ok := value.(string); ok {
				image.Caption = caption
			} else {
				fmt.Println("caption is not defined")
			}
		case "album_id":
			if id, ok := value.(string); ok {
				album_id = id
			} else {
				fmt.Println("album id is not defined")
			}
		case "upload_type":
			if uploadType, ok := value.(string); ok {
				image.UploadType = uploadType
			} else {
				fmt.Println("upload type not defined")
			}
		case "captured_at":
			if capturedAt, ok := value.(string); ok {
				layout := time.RFC3339 // For ISO8601 format
				parsedTime, err := time.Parse(layout, capturedAt)
				if err != nil {
					fmt.Println("Error parsing datetime:", err)
				} else {
					image.CapturedAt = parsedTime
				}
			} else {
				fmt.Println("upload type not defined")
			}
		}
	}

	imageCreationQuery := `INSERT INTO images
			  (image_id, image_owner, caption, upload_type, captured_at) VALUES ($1,(SELECT user_id FROM users WHERE auth_zero_id=$2), $3, $4, $5)
			  RETURNING image_id, created_at`
	err = connPool.Pool.QueryRow(ctx, imageCreationQuery, image.ID, image.ImageOwner, image.Caption,
		image.UploadType, image.CapturedAt).Scan(&image.ID, &image.CapturedAt)
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

	getUploaderData := `SELECT first_name, last_name, user_id FROM users WHERE auth_zero_id=$1`
	err = connPool.Pool.QueryRow(ctx, getUploaderData, image.ImageOwner).Scan(&image.FirstName, &image.LastName, &image.ImageOwner)
	if err != nil {
		WriteErrorToWriter(w, "Unable to get uploader data")
		log.Printf("Unable to get uploader data: %v", err)
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

func DELETEImageData(ctx context.Context, connPool *m.PGPool, imageID string, uid string) error {
	query := `DELETE FROM images WHERE image_id = $1 AND image_owner = (SELECT user_id FROM users WHERE auth_zero_id = $2)`

	result, err := connPool.Pool.Exec(ctx, query, imageID, uid)
	if err != nil {
		return err
		//return errors.New("error deleting image details")
	}

	if result.RowsAffected() < 1 {
		return errors.New("no content found for lookup")
	}

	return nil

}

func PATCHUpdateImageAlbum(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	imageID := r.URL.Query().Get("image_id")
	albumID := r.URL.Query().Get("album_id")

	log.Print(imageID)
	log.Print(albumID)
	log.Print(uid)

	updateQuery := `UPDATE imagealbum AS ia
					SET album_id = $1
					FROM images AS i, albums AS a
					WHERE ia.image_id = i.image_id
					AND ia.album_id = a.album_id
					AND ia.image_id = $2
					AND a.revealed_at > NOW() AT TIME ZONE 'UTC'
					AND i.image_owner = (SELECT user_id FROM users WHERE auth_zero_id=$3)`

	tag, err := connPool.Pool.Exec(ctx, updateQuery, albumID, imageID, uid)
	if err != nil {
		responseBytes := []byte("Error attempting to change image album\"")

		w.WriteHeader(401)
		w.Header().Set("Content-Type", "application/json") // add content length number of bytes
		w.Write(responseBytes)
		return
	}

	if tag.RowsAffected() == 0 {
		responseBytes := []byte("Couldn't update album")

		w.WriteHeader(403)
		w.Header().Set("Content-Type", "application/json") // add content length number of bytes
		w.Write(responseBytes)
		return
	}

	responseBytes := []byte("Success updating album")

	w.Header().Set("Content-Type", "application/json") //add content length number of bytes
	w.Write(responseBytes)

}

func QueryImagesData(ctx context.Context, connPool *m.PGPool, album *m.Album, uid string) {
	imageQuery := `SELECT i.image_id, i.image_owner, u.first_name, u.last_name, i.caption, i.upload_type,
                      (SELECT COUNT(*) FROM likes l WHERE l.image_id = i.image_id) AS like_count,
                      (SELECT COUNT(*) FROM upvotes up WHERE up.image_id = i.image_id) AS upvote_count,
                      EXISTS (SELECT 1 FROM likes l WHERE l.image_id = i.image_id AND l.user_id = (SELECT user_id FROM users WHERE auth_zero_id = $2)) AS user_has_liked,
                      EXISTS (SELECT 1 FROM upvotes up WHERE up.image_id = i.image_id AND up.user_id = (SELECT user_id FROM users WHERE auth_zero_id = $2)) AS user_has_upvoted,
                      i.created_at
					  FROM images i
					  JOIN imagealbum ia ON i.image_id = ia.image_id
					  JOIN users u ON i.image_owner = u.user_id
					  WHERE ia.album_id = $1`

	images := []m.Image{}

	//Fetch Albums Images
	imageResponse, err := connPool.Pool.Query(ctx, imageQuery, album.AlbumID, uid)
	if err != nil {
		log.Print(err)
	}
	defer imageResponse.Close()

	//Scan through images in album
	for imageResponse.Next() {
		var image m.Image

		err = imageResponse.Scan(&image.ID, &image.ImageOwner, &image.FirstName, &image.LastName, &image.Caption,
			&image.UploadType, &image.Likes, &image.Upvotes, &image.UserLiked, &image.UserUpvoted, &image.CapturedAt)
		if err != nil {
			log.Print(err)
		}

		images = append(images, image)
	}
	album.Images = images
}
