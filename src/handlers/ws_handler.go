package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	m "last_weekend_services/src/models"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1048,
	WriteBufferSize: 1048,
}

type ConnectionState struct {
	Conn   *websocket.Conn
	Active bool
}

type WebSocketPayload struct {
	Operation string      `json:"operation"`
	Type      string      `json:"type"`
	Received  time.Time   `json:"received"`
	UserID    string      `json:"user_id"`
	Payload   interface{} `json:"payload"`
}

var uid = "69ac1008-60f8-4518-8039-e332c9265115"

func WebSocketEndpointHandler(connPool *m.PGPool, rdb *redis.Client, ctx context.Context) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WebSocket(w, r, connPool, rdb, ctx)
	})
}

func WebSocket(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, ctx context.Context) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "failed to upgrade websocket: %s", err)
		return
	}

	var newConnection ConnectionState = ConnectionState{Conn: conn, Active: true}

	log.Print("Listening via WebSocket...")
	go newConnection.ListenAndWrite(ctx, connPool, conn, rdb)
	go newConnection.CheckConnectionStatus(ctx, conn)

}

func (connectionState *ConnectionState) ListenAndWrite(ctx context.Context, connPool *m.PGPool, conn *websocket.Conn, rdb *redis.Client) {
	//queryTime := time.Now().UTC()
	//updatedTime := queryTime

	for connectionState.Active == true {

		pubsub := rdb.Subscribe(ctx, "album-requests")
		ch := pubsub.Channel()

		for message := range ch {
			var wsPayload WebSocketPayload
			json.Unmarshal([]byte(message.Payload), &wsPayload)

			//log.Print(wsPayload.UserID)
			if wsPayload.UserID == uid {
				err := conn.WriteMessage(websocket.TextMessage, []byte(message.Payload))
				if err != nil {
					log.Print(err)
					return
				}
			}
		}

	}
	log.Print("The websocket is closing..")
	conn.Close()
	return
}

func (connectionState *ConnectionState) CheckConnectionStatus(ctx context.Context, conn *websocket.Conn) {

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err) {
				log.Printf("error: %v", err)
				connectionState.Active = false
				return
			}
		}
	}
}

func FriendRequestCheck(ctx context.Context, connPool *m.PGPool, conn *websocket.Conn, queryTime *time.Time, updatedTime *time.Time) error {
	var wsPayload WebSocketPayload
	var user User
	var receivedLocal time.Time

	notificationQuery := `SELECT fr.sender_id, u.first_name, u.last_name, fr.requested_at
						  FROM users u
						  JOIN friend_requests fr ON fr.sender_id = u.user_id
						  WHERE fr.receiver_id = $1 AND fr.requested_at > $2`
	rows, err := connPool.Pool.Query(ctx, notificationQuery, uid, *queryTime)
	if err != nil {
		return err
	}
	for rows.Next() {
		wsPayload.Type = "friend_request"
		wsPayload.Operation = "INSERT"

		err := rows.Scan(&user.ID, &user.FirstName, &user.LastName, &receivedLocal)
		if err != nil {
			return err
		}

		wsPayload.Payload = user
		wsPayload.Received = receivedLocal.UTC()

		if wsPayload.Received.After(*updatedTime) {
			*updatedTime = wsPayload.Received
		}
		if err := writeNotification(conn, wsPayload); err != nil {
			return err
		}
	}
	return nil
}

/*func NotificationCheck(ctx context.Context, connPool *m.PGPool, conn *websocket.Conn, queryTime *time.Time, updatedTime *time.Time) error {
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
		return err
	}
	for rows.Next() {
		wsPayload.Type = "generic_notification"
		wsPayload.Operation = "INSERT"

		err := rows.Scan(&user.ID, &user.FirstName, &user.LastName, &genericNotification.MediaID, &genericNotification.AlbumName, &genericNotification.NotificationType, &genericNotification.NotificationSeen, &receivedLocal)
		if err != nil {
			return err
		}
		genericNotification.Notifier = user
		wsPayload.Payload = genericNotification
		wsPayload.Received = receivedLocal.UTC()

		if wsPayload.Received.After(*updatedTime) {
			*updatedTime = wsPayload.Received
		}
		if err := writeNotification(conn, wsPayload); err != nil {
			return err
		}
	}
	return nil
}*/

func writeNotification(conn *websocket.Conn, n WebSocketPayload) error {
	responseBytes, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return err
	}
	err = conn.WriteMessage(websocket.TextMessage, responseBytes)

	if err != nil {
		if websocket.IsUnexpectedCloseError(err) {
			log.Println("Warning: The server unexpectedly closed!")
			conn.Close()
			return err
		}
		log.Print(err)
		return err
	}
	log.Printf("Sent: %v, Operation: %v, Received At: %v\n", n.Type, n.Operation, n.Received)
	if err != nil {
		log.Print(err)

	}
	return err
}
