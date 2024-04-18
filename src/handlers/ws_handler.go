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
	AlbumID   string      `json:"album_ID"`
	Payload   interface{} `json:"payload"`
}

func WebSocketEndpointHandler(connPool *m.PGPool, rdb *redis.Client, ctx context.Context) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
		}
		switch r.URL.Path {
		case "/ws":
			WebSocket(w, r, connPool, rdb, ctx, claims.RegisteredClaims.Subject, "notifications")
		case "/ws/album":
			var channel string = r.URL.Query().Get("channel")
			WebSocket(w, r, connPool, rdb, ctx, claims.RegisteredClaims.Subject, channel)
		}
	})
}

func WebSocket(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, rdb *redis.Client, ctx context.Context, auth0UID string, channel string) {
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

	//log.Printf("Listening via %v WebSocket...", channel)

	quit := make(chan int)
	go newConnection.ListenAndWrite(ctx, conn, rdb, uid, quit, channel)
	go newConnection.CheckConnectionStatus(ctx, conn, quit)

}

func (connectionState *ConnectionState) ListenAndWrite(ctx context.Context, conn *websocket.Conn, rdb *redis.Client, uid string, quit chan int, channel string) {
	pubSub := rdb.Subscribe(ctx, channel)
	for connectionState.Active == true {

		notificationChannel := pubSub.Channel(redis.WithChannelSize(250))

		select {
		case message := <-notificationChannel:
			err := sendWebSocketNotification(conn, message, uid, channel)
			if err != nil {
				log.Printf("ListenAndWriteError: %v", err)
				return
			}
		case <-quit:
			connectionState.Active = false
		}
	}

	//log.Printf("The %v websocket is closing..", channel)
	err := pubSub.Close()
	if err != nil {
		log.Printf("Error closing redis channel: %v with error: %v", channel, err)
		return
	}
	err = conn.Close()
	if err != nil {
		log.Printf("Error closing websocket: %v", err)
		return
	}
	return
}

func (connectionState *ConnectionState) CheckConnectionStatus(ctx context.Context, conn *websocket.Conn, quit chan int) {

	for {
		message, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err) {
				log.Printf("error: %v", err)
				quit <- 0
				return
			}
		}
		if message == 1000 {
			quit <- 0
			log.Printf("Closing: %v", message)
			return
		}
	}
}

func sendWebSocketNotification(conn *websocket.Conn, message *redis.Message, uid string, channel string) error {
	var wsPayload WebSocketPayload
	err := json.Unmarshal([]byte(message.Payload), &wsPayload)
	if err != nil {
		return err
	}

	switch channel {
	case "notifications":
		if wsPayload.UserID == uid {
			err = conn.WriteMessage(websocket.TextMessage, []byte(message.Payload))
			if err != nil {
				log.Printf("sendWebSocketNotification: %v", err)
				return err
			}
		}
		return nil
	case wsPayload.AlbumID:
		if wsPayload.AlbumID == channel {
			err = conn.WriteMessage(websocket.TextMessage, []byte(message.Payload))
			if err != nil {
				log.Printf("sendWebSocketNotification: %v", err)
				return err
			}
		}
		return nil
	}
	return nil
}
