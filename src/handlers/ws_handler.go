package handlers

import (
	"context"
	"log"
	"net/http"

	//"github.com/google/uuid"
	m "last_weekend_services/src/models"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgconn"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1048,
	WriteBufferSize: 1048,
}

func WebSocketHandler(w http.ResponseWriter, r *http.Request, connPool *m.PGPool, ctx context.Context) {
	_, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print(err)
	}

	/*pgPoolConn, err := connPool.Pool.Acquire(ctx)
	if err != nil {
		log.Print(err)
	}

	for {
		log.Print("\nWaiting for Notification...")
		n, err := pgConn.WaitForNotification(ctx)
		if err != nil {
			log.Print(err)
		}
		HandleNotifications(conn, n)
	}*/

}

func HandleNotifications(conn *websocket.Conn, n *pgconn.Notification) {
	log.Print(n.Payload)

	responseBytes := []byte(n.Payload)

	err := conn.WriteMessage(websocket.TextMessage, responseBytes)
	if err != nil {
		log.Print(err)
	}

}
