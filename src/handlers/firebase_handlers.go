package handlers

import (
	"context"
	"firebase.google.com/go/v4/messaging"
	"fmt"
	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	m "last_weekend_services/src/models"
	"log"
	"net/http"
)

func FirebaseHandlers(connPool *m.PGPool, ctx context.Context) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodPut:
			switch r.URL.Path {
			case "/fcm":
				PUTFirebaseToken(w, r, ctx, connPool, claims.RegisteredClaims.Subject)
			}
		}

	})
}

func PUTFirebaseToken(w http.ResponseWriter, r *http.Request, context context.Context, connPool *m.PGPool, authZeroID string) {
	token := r.URL.Query().Get("token")
	deviceId := r.URL.Query().Get("device_id")

	query := `INSERT INTO firebase_tokens (user_id, token, device_id)
				VALUES ((SELECT user_id FROM users WHERE auth_zero_id=$1), $2, $3)
				ON CONFLICT (user_id,device_id)
				DO UPDATE SET token = EXCLUDED.token,  updated_at = (now() AT TIME ZONE 'utc'::text)`

	_, err := connPool.Pool.Exec(context, query, authZeroID, token, deviceId)
	if err != nil {
		log.Printf("Failed to insert firebase token: %v", err)
	}

	responseBytes := []byte("updated token - success")

	w.Header().Set("Content-Type", "application/json") // add content length number of bytes
	w.Write(responseBytes)

}

func SendFirebaseMessageToUID(context context.Context, connPool *m.PGPool, messagingClient *messaging.Client, notification m.FirebaseNotification) error {
	var title string
	var body string
	var tokens []string

	tokenQuery := `SELECT token FROM firebase_tokens WHERE user_id = $1`

	rows, err := connPool.Pool.Query(context, tokenQuery, notification.RecipientID)
	if err != nil {
		log.Print(err)
	}

	for rows.Next() {
		var token string

		err = rows.Scan(&token)
		if err != nil {
			log.Print(err)
		}

		tokens = append(tokens, token)
	}

	switch notification.Type {
	case "album-invite":
		title = fmt.Sprintf("Accept invite to %v", notification.ContentName)
		body = fmt.Sprintf("%v sent you an album invite.", notification.RequesterName)
	}

	fcmNotification := messaging.Notification{
		Title: title,
		Body:  body,
	}

	message := messaging.MulticastMessage{
		Data:         nil,
		Tokens:       tokens,
		Notification: &fcmNotification,
	}

	_, err = messagingClient.SendEachForMulticast(context, &message)
	if err != nil {
		return err
	}

	return nil
}
