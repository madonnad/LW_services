package handlers

import (
	"context"
	"encoding/json"
	"fmt"
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

	for _, item := range likedSummary {
		notifications.SummaryNotifications = append(notifications.SummaryNotifications, item)
	}
	for _, item := range upvotedSummary {
		notifications.SummaryNotifications = append(notifications.SummaryNotifications, item)
	}
	notifications.FriendRequests = friendRequests
	notifications.AlbumRequests = albumRequests

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
	albumRequestQuery := `
						SELECT ar.album_id, a.album_name, a.album_cover_id, a.album_owner, u.first_name, u.last_name, ar.invited_at
						FROM album_requests ar
						JOIN albums a ON a.album_id = ar.album_id
						JOIN users u ON u.user_id = a.album_owner
						WHERE ar.invited_id = $1`

	rows, err := connPool.Pool.Query(ctx, albumRequestQuery, uid)
	if err != nil {
		fmt.Fprintf(w, "Failed to get query DB: %v", err)
		return nil, err
	}

	for rows.Next() {
		var request m.AlbumRequestNotification

		err := rows.Scan(&request.AlbumID, &request.AlbumName, &request.AlbumCoverID, &request.AlbumOwner, &request.OwnerFirst, &request.OwnerLast, &request.ReceivedAt)
		if err != nil {
			fmt.Fprintf(w, "Failed to insert data to object: %v", err)
			return nil, err
		}

		albumRequests = append(albumRequests, request)
	}
	return albumRequests, nil
}

func QueryFriendRequests(ctx context.Context, w http.ResponseWriter, connPool *m.PGPool, uid string) ([]m.FriendRequestNotification, error) {
	var friendRequests []m.FriendRequestNotification
	friendRequestQuery := `
						SELECT fr.sender_id, u.first_name, u.last_name, fr.requested_at
						FROM users u
						JOIN friend_requests fr ON fr.sender_id = u.user_id
						WHERE fr.receiver_id = $1`

	rows, err := connPool.Pool.Query(ctx, friendRequestQuery, uid)
	if err != nil {
		fmt.Fprintf(w, "Failed to get query DB: %v", err)
		return nil, err
	}

	for rows.Next() {
		var request m.FriendRequestNotification

		err := rows.Scan(&request.UserID, &request.FirstName, &request.LastName, &request.ReceivedAt)
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
	genericNotificationQuery := `
					WITH AlbumTotals AS (
					SELECT album_id, COUNT(*) AS total
					FROM notifications
					WHERE notification_type = $2
					GROUP BY album_id)
					
					SELECT u.first_name, a.album_name, n.album_id, a.album_cover_id, n.notification_type, n.received_at, at.total
					FROM (
						SELECT sender_id, album_id, notification_seen, receiver_id, notification_type, received_at,
							row_number() OVER (PARTITION BY album_id ORDER BY received_at DESC) AS recent
						FROM notifications) AS n
					JOIN users u
					ON sender_id = u.user_id
					JOIN albums a
					ON a.album_id = n.album_id
					JOIN AlbumTotals at
					ON n.album_id = at.album_id
					WHERE recent <= 2 
					AND receiver_id = $1
					AND notification_type = $2
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

		existingSummary := findSummaryByIDType(summaryNotifications, summary.AlbumID, summary.NotificationType)
		if existingSummary != nil {
			existingSummary.NameTwo = summary.NameOne
			continue
		}
		summaryNotifications = append(summaryNotifications, summary)
	}

	return summaryNotifications, nil
}

func findSummaryByIDType(slice []m.SummaryNotification, id string, notificationType string) *m.SummaryNotification {
	for _, item := range slice {
		if item.AlbumID == id && item.NotificationType == notificationType {
			return &item
		}
	}
	return nil
}