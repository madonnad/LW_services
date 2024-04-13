package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	m "last_weekend_services/src/models"
	"log"
	"net/http"
	"time"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/redis/go-redis/v9"
)

func NotificationsEndpointHandler(ctx context.Context, connPool *m.PGPool, rdb *redis.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			fmt.Fprintf(w, "Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodGet:
			GETExistingNotifications(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
		case http.MethodPatch:
			PATCHMarkNotificationSeen(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
		}

	})
}

func GETExistingNotifications(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	var notifications m.Notification

	searchDate := time.Now().AddDate(0, -6, 0).Format("2006-01-02 15:04:05")

	likedSummary, _ := QuerySummaryNotifications(ctx, w, connPool, uid, searchDate, "liked")
	upvotedSummary, _ := QuerySummaryNotifications(ctx, w, connPool, uid, searchDate, "upvote")
	friendRequests, _ := QueryFriendRequests(ctx, w, connPool, uid)
	albumRequests, _ := QueryAlbumRequests(ctx, w, connPool, uid)
	albumRequestsResponses, _ := QueryAlbumRequestResponses(ctx, w, connPool, uid)

	for _, item := range likedSummary {
		notifications.SummaryNotifications = append(notifications.SummaryNotifications, item)
	}
	for _, item := range upvotedSummary {
		notifications.SummaryNotifications = append(notifications.SummaryNotifications, item)
	}
	notifications.FriendRequests = friendRequests
	notifications.AlbumRequests = albumRequests
	notifications.AlbumRequestResponses = albumRequestsResponses

	responseBytes, err := json.MarshalIndent(notifications, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)

}

func QueryAlbumRequests(ctx context.Context, w http.ResponseWriter, connPool *m.PGPool, uid string) ([]m.AlbumRequestNotification, error) {
	var albumRequests []m.AlbumRequestNotification

	// Looks up any accepted or pending requests from logged in user
	queryAlbumInvites := `
						SELECT ar.request_id, ar.album_id, a.album_name, a.album_cover_id, a.album_owner, u.first_name,
						       		u.last_name, u2.user_id, u2.first_name, u2.last_name ,ar.updated_at, a.unlocked_at,
						       		ar.invite_seen, ar.response_seen, ar.status
						FROM album_requests ar
						JOIN albums a ON a.album_id = ar.album_id
						JOIN users u ON u.user_id = a.album_owner
						JOIN users u2 ON ar.invited_id = u2.user_id
						WHERE invited_id = (SELECT user_id FROM users WHERE auth_zero_id=$1)
						AND (ar.status = 'pending' OR ar.status ='accepted')`

	rows, err := connPool.Pool.Query(ctx, queryAlbumInvites, uid)
	if err != nil {
		fmt.Fprintf(w, "Failed to get query DB: %v", err)
		return nil, err
	}

	for rows.Next() {
		var request m.AlbumRequestNotification

		err := rows.Scan(&request.RequestID, &request.AlbumID, &request.AlbumName, &request.AlbumCoverID,
			&request.AlbumOwner, &request.OwnerFirst, &request.OwnerLast, &request.GuestID, &request.GuestFirst,
			&request.GuestLast, &request.ReceivedAt, &request.UnlockedAt,
			&request.InviteSeen, &request.ResponseSeen, &request.Status)
		if err != nil {
			fmt.Fprintf(w, "Failed to insert data to object AlbumInvites: %v", err)
			return nil, err
		}

		albumRequests = append(albumRequests, request)
	}
	return albumRequests, nil
}

func QueryAlbumRequestResponses(ctx context.Context, w http.ResponseWriter, connPool *m.PGPool, uid string) ([]m.AlbumRequestNotification, error) {
	var albumRequestResponses []m.AlbumRequestNotification
	querySentAlbumInviteResponses := `SELECT ar.request_id, ar.album_id, a.album_name, a.album_cover_id, a.album_owner, 
       									u2.first_name, u2.last_name, ar.invited_id, u.first_name, u.last_name, 
       									ar.updated_at, a.unlocked_at, ar.invite_seen, ar.response_seen, ar.status
									FROM album_requests ar
									JOIN albums a ON a.album_id = ar.album_id
									JOIN users u ON ar.invited_id = u.user_id
									JOIN users u2 on a.album_owner = u2.user_id
									WHERE (a.album_owner = (SELECT user_id FROM users WHERE auth_zero_id=$1)
									AND ar.status='accepted')`

	rows, err := connPool.Pool.Query(ctx, querySentAlbumInviteResponses, uid)
	if err != nil {
		fmt.Fprintf(w, "Failed to get query DB AlbumReqs: %v", err)
		return nil, err
	}

	for rows.Next() {
		var request m.AlbumRequestNotification

		err := rows.Scan(&request.RequestID, &request.AlbumID, &request.AlbumName, &request.AlbumCoverID,
			&request.AlbumOwner, &request.OwnerFirst, &request.OwnerLast, &request.GuestID, &request.GuestFirst,
			&request.GuestLast, &request.ReceivedAt, &request.UnlockedAt, &request.InviteSeen, &request.ResponseSeen, &request.Status)
		if err != nil {
			fmt.Fprintf(w, "Failed to insert data to object: %v", err)
			return nil, err
		}

		albumRequestResponses = append(albumRequestResponses, request)
	}
	return albumRequestResponses, nil
}

func QueryFriendRequests(ctx context.Context, w http.ResponseWriter, connPool *m.PGPool, uid string) ([]m.FriendRequestNotification, error) {
	var friendRequests []m.FriendRequestNotification
	batch := &pgx.Batch{}
	pendingFriendRequests := `SELECT fr.request_id, fr.sender_id, fr.receiver_id, u.first_name, u.last_name, fr.updated_at, fr.status, fr.seen
								FROM users u
								JOIN friend_requests fr ON fr.sender_id = u.user_id
								WHERE fr.receiver_id = (SELECT user_id FROM users WHERE auth_zero_id=$1)
								AND fr.status = 'pending'`
	acceptedFriendRequests := `SELECT fr.request_id, fr.receiver_id, fr.sender_id, u.first_name, u.last_name, fr.updated_at, fr.status, fr.seen
								FROM users u 
								JOIN friend_requests fr ON fr.receiver_id = u.user_id
								WHERE fr.sender_id = (SELECT user_id FROM users WHERE auth_zero_id=$1)
								AND fr.status = 'accepted'`

	batch.Queue(pendingFriendRequests, uid)
	batch.Queue(acceptedFriendRequests, uid)
	batchResults := connPool.Pool.SendBatch(ctx, batch)

	pendingRows, err := batchResults.Query()
	if err != nil {
		fmt.Fprintf(w, "Failed to get pending friend requests: %v", err)
		return nil, err
	}

	for pendingRows.Next() {
		var request m.FriendRequestNotification

		err := pendingRows.Scan(&request.RequestID, &request.SenderID, &request.ReceiverID, &request.FirstName, &request.LastName, &request.ReceivedAt, &request.Status, &request.RequestSeen)
		if err != nil {
			fmt.Fprintf(w, "Failed to insert data to object: %v", err)
			return nil, err
		}
		friendRequests = append(friendRequests, request)
	}

	acceptedRows, err := batchResults.Query()
	if err != nil {
		fmt.Fprintf(w, "Failed to get accepted friend requests: %v", err)
		return nil, err
	}

	for acceptedRows.Next() {
		var request m.FriendRequestNotification

		err := acceptedRows.Scan(&request.RequestID, &request.ReceiverID, &request.SenderID, &request.FirstName, &request.LastName, &request.ReceivedAt, &request.Status, &request.RequestSeen)
		if err != nil {
			fmt.Fprintf(w, "Failed to insert data to object: %v", err)
			return nil, err
		}
		friendRequests = append(friendRequests, request)
	}

	return friendRequests, nil
}

func QuerySummaryNotifications(ctx context.Context, w http.ResponseWriter, connPool *m.PGPool, uid string, dateString string, searchType string) ([]m.SummaryNotification, error) {
	var summaryNotifications []m.SummaryNotification
	genericNotificationQuery := `WITH AlbumTotals AS (
								SELECT album_id, COUNT(*) AS total
								FROM notifications
								WHERE type = $2
								GROUP BY album_id)
								
								SELECT u.first_name, a.album_name, n.album_id, a.album_cover_id, n.type, n.received_at, at.total
								FROM (
								SELECT sender_id, album_id, seen, receiver_id, type, received_at,
									row_number() OVER (PARTITION BY album_id ORDER BY received_at DESC) AS recent
								FROM notifications) AS n
								JOIN users u
								ON sender_id = u.user_id
								JOIN albums a
								ON a.album_id = n.album_id
								JOIN AlbumTotals at
								ON n.album_id = at.album_id
								WHERE recent <= 2
								AND receiver_id = (SELECT user_id FROM users WHERE auth_zero_id=$1)
								AND type = $2
								AND received_at > $3`

	rows, err := connPool.Pool.Query(ctx, genericNotificationQuery, uid, searchType, dateString)
	if err != nil {
		fmt.Fprintf(w, "Failed to get query DB: %v", err)
		return nil, err
	}

	for rows.Next() {
		var summary m.SummaryNotification

		err := rows.Scan(&summary.NameOne, &summary.AlbumName, &summary.AlbumID, &summary.AlbumCoverID, &summary.NotificationType, &summary.ReceivedAt, &summary.AlbumTypeTotal)
		if err != nil {
			fmt.Fprintf(w, "Failed to insert data to object: %v", err)
			return nil, err
		}

		existingSummary := lookupSummaryByAlbumAndType(summaryNotifications, summary.AlbumID, summary.NotificationType)
		if existingSummary != nil {
			existingSummary.NameTwo = summary.NameOne
			continue
		}
		summaryNotifications = append(summaryNotifications, summary)
	}

	return summaryNotifications, nil
}

// The following function will check to see if a summary for an Album and Noti type combination is already created to append to it
func lookupSummaryByAlbumAndType(slice []m.SummaryNotification, id string, notificationType string) *m.SummaryNotification {
	for _, item := range slice {
		if item.AlbumID == id && item.NotificationType == notificationType {
			return &item
		}
	}
	return nil
}

func PATCHMarkNotificationSeen(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	notificationID := r.URL.Query().Get("id")

	markSeenQuery := `UPDATE notifications
						SET seen = true
						WHERE (notification_uid = $1 
						           AND receiver_id = (SELECT user_id FROM users WHERE auth_zero_id=$2))`

	_, err := connPool.Pool.Exec(ctx, markSeenQuery, notificationID, uid)
	if err != nil {
		fmt.Fprintf(w, "Error trying to mark friend request as seen: %v", err)
		return
	}

	responseBytes, err := json.MarshalIndent("friend request successfully seen", "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}
