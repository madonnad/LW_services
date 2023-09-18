package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	m "last_weekend_services/src/models"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1048,
	WriteBufferSize: 1048,
}

type WebSocketPayload struct {
	Operation string      `json:"operation"`
	Type      string      `json:"type"`
	Received  time.Time   `json:"received"`
	Payload   interface{} `json:"payload"`
}

var uid = "69ac1008-60f8-4518-8039-e332c9265115"

func WebSocketHandler(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print(err)
	}
	log.Print("Listening via WebSocket...")
	queryTime := time.Now().UTC()
	updatedTime := queryTime

	for {
		AlbumRequestCheck(ctx, connPool, conn, &queryTime, &updatedTime)
		FriendRequestCheck(ctx, connPool, conn, &queryTime, &updatedTime)
		NotificationCheck(ctx, connPool, conn, &queryTime, &updatedTime)

		time.Sleep(4 * time.Second)
		queryTime = updatedTime
	}

}

func AlbumRequestCheck(ctx context.Context, connPool *m.PGPool, conn *websocket.Conn, queryTime *time.Time, updatedTime *time.Time) {
	var wsPayload WebSocketPayload
	var album Album
	var receivedLocal time.Time

	notificationQuery := `SELECT a.album_id, a.album_name, a.album_cover_id, a.album_owner, ar.invited_at
						 FROM albums a
						 JOIN album_requests ar ON a.album_id = ar.album_id
						 WHERE ar.invited_id = $1 AND ar.invited_at > $2`

	rows, err := connPool.Pool.Query(ctx, notificationQuery, uid, *queryTime)
	if err != nil {
		log.Print(err)
	}

	for rows.Next() {
		wsPayload.Type = "album_request"
		wsPayload.Operation = "INSERT"

		err := rows.Scan(&album.AlbumID, &album.AlbumName, &album.AlbumCoverID, &album.AlbumOwner, &receivedLocal)
		if err != nil {
			log.Print(err)
		}

		wsPayload.Payload = album
		wsPayload.Received = receivedLocal.UTC()

		if wsPayload.Received.After(*updatedTime) {
			*updatedTime = wsPayload.Received
		}
		HandleNotifications(conn, wsPayload)
	}
}

func FriendRequestCheck(ctx context.Context, connPool *m.PGPool, conn *websocket.Conn, queryTime *time.Time, updatedTime *time.Time) {
	var wsPayload WebSocketPayload
	var user User
	var receivedLocal time.Time

	log.Print(queryTime)

	notificationQuery := `SELECT fr.sender_id, u.first_name, u.last_name, fr.requested_at
						  FROM users u
						  JOIN friend_requests fr ON fr.sender_id = u.user_id
						  WHERE fr.receiver_id = $1 AND fr.requested_at > $2`

	rows, err := connPool.Pool.Query(ctx, notificationQuery, uid, *queryTime)
	if err != nil {
		log.Print(err)
	}

	for rows.Next() {
		wsPayload.Type = "friend_request"
		wsPayload.Operation = "INSERT"

		err := rows.Scan(&user.ID, &user.FirstName, &user.LastName, &receivedLocal)
		if err != nil {
			log.Print(err)
		}

		wsPayload.Payload = user
		wsPayload.Received = receivedLocal.UTC()

		if wsPayload.Received.After(*updatedTime) {
			*updatedTime = wsPayload.Received
		}
		HandleNotifications(conn, wsPayload)
	}
}

func NotificationCheck(ctx context.Context, connPool *m.PGPool, conn *websocket.Conn, queryTime *time.Time, updatedTime *time.Time) {
	var wsPayload WebSocketPayload
	var user User
	var genericNotification m.GenericNotification
	var receivedLocal time.Time

	notificationQuery := `SELECT n.sender_id, u.first_name, u.last_name, n.media_id, a.album_name, n.notification_type, n.notification_seen, n.received_at
						  FROM users u
						  JOIN notifications n ON n.sender_id = u.user_id
						  JOIN albums a ON n.album_id = a.album_id
						  WHERE n.receiver_id = $1 AND n.received_at > $2`

	rows, err := connPool.Pool.Query(ctx, notificationQuery, uid, queryTime)
	if err != nil {
		log.Print(err)
	}

	for rows.Next() {
		wsPayload.Type = "generic_notification"
		wsPayload.Operation = "INSERT"

		err := rows.Scan(&user.ID, &user.FirstName, &user.LastName, &genericNotification.MediaID, &genericNotification.AlbumName, &genericNotification.NotificationType, &genericNotification.NotificationSeen, &receivedLocal)
		if err != nil {
			log.Print(err)
		}
		genericNotification.Notifier = user
		wsPayload.Payload = genericNotification
		wsPayload.Received = receivedLocal.UTC()

		if wsPayload.Received.After(*updatedTime) {
			*updatedTime = wsPayload.Received
		}
		HandleNotifications(conn, wsPayload)
	}
}

func HandleNotifications(conn *websocket.Conn, n WebSocketPayload) {
	responseBytes, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		log.Print(err)
	}

	err = conn.WriteMessage(websocket.TextMessage, responseBytes)
	log.Printf("Sent: %v, Operation: %v, Received At: %v\n", n.Type, n.Operation, n.Received)
	if err != nil {
		log.Print(err)
	}

}
