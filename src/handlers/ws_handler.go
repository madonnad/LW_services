package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	m "last_weekend_services/src/models"
	"log"
	"net/http"

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
	UserID    string      `json:"user_id"`
	Payload   interface{} `json:"payload"`
}

//ar uid = "69ac1008-60f8-4518-8039-e332c9265115"

func WebSocketEndpointHandler(connPool *m.PGPool, rdb *redis.Client, ctx context.Context) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
		}
		WebSocket(w, r, connPool, rdb, ctx, claims.RegisteredClaims.Subject)
	})
}

func WebSocket(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, ctx context.Context, auth0UID string) {
	var uid string
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "failed to upgrade websocket: %s", err)
		return
	}

	uidQuery := `SELECT user_id FROM users WHERE auth_zero_id = $1`
	err = connPool.Pool.QueryRow(ctx, uidQuery, auth0UID).Scan(&uid)
	if err != nil {
		WriteErrorToWriter(w, "Error: Unable to lookup requesting user")
		log.Printf("Unable to lookup requesting user: %v", err)
		return
	}

	var newConnection = ConnectionState{Conn: conn, Active: true}

	log.Print("Listening via WebSocket...")
	go newConnection.ListenAndWrite(ctx, conn, rdb, uid)
	go newConnection.CheckConnectionStatus(ctx, conn)

}

func (connectionState *ConnectionState) ListenAndWrite(ctx context.Context, conn *websocket.Conn, rdb *redis.Client, uid string) {
	//queryTime := time.Now().UTC()
	//updatedTime := queryTime

	for connectionState.Active == true {

		pubSub := rdb.Subscribe(ctx, "notifications")
		notificationChannel := pubSub.Channel()

		// Handle All Notifications to WebSocket Notification
		for message := range notificationChannel {
			err := sendWebSocketNotification(conn, message, uid)
			if err != nil {
				log.Print(err)
				return
			}
		}

	}
	log.Print("The websocket is closing..")
	conn.Close()
	return
}

func (connectionState *ConnectionState) CheckConnectionStatus(ctx context.Context, conn *websocket.Conn) {

	for {
		messageType, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err) {
				log.Printf("error: %v", err)
				connectionState.Active = false
				return
			}
		}

		if messageType == 1000 {
			connectionState.Active = false
			return
		}
	}
}

func sendWebSocketNotification(conn *websocket.Conn, message *redis.Message, uid string) error {
	var wsPayload WebSocketPayload
	err := json.Unmarshal([]byte(message.Payload), &wsPayload)
	if err != nil {
		return err
	}

	// TODO: May need to look into moving this higher up the function - it may get busy if every user has to check this
	if wsPayload.UserID == uid {
		err = conn.WriteMessage(websocket.TextMessage, []byte(message.Payload))
		if err != nil {
			log.Print(err)
			return err
		}
	}
	return nil
}
