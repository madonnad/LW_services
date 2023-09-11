package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1048,
	WriteBufferSize: 1048,
}

func (wsPool *WSPool) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print(err)
	}

	uid, err := uuid.Parse(r.URL.Query().Get("uid"))
	if err != nil {
		log.Print(err)
	}

	poolItem := WSConn{conn: conn, uid: uid}

	wsPool.pool = append(wsPool.pool, poolItem)

	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			log.Print(err)
			if websocket.IsUnexpectedCloseError(err) {
				fmt.Println("Warning: The server unexpectedly closed!")
				conn.Close()
				return
			}
		}

		switch messageType {
		case websocket.TextMessage:
			fmt.Printf("Message: %v\n", string(p))
		}

		responseBytes := []byte("Hi right back at ya!")

		err = conn.WriteMessage(websocket.TextMessage, responseBytes)
		if err != nil {
			log.Print(err)
		}
	}

}
