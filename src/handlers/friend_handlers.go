package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	m "last_weekend_services/src/models"
	"log"
	"net/http"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/redis/go-redis/v9"
)

func FriendEndpointHanlder(ctx context.Context, connPool *m.PGPool, rdb *redis.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
		}

		switch r.Method {
		case http.MethodGet:
			GETFriendsByUserID(ctx, w, r, connPool, claims.RegisteredClaims.Subject)
		}
	})
}

func GETFriendsByUserID(ctx context.Context, w http.ResponseWriter, r *http.Request, connPool *m.PGPool, uid string) {
	var friends []m.Friend

	query := `
			SELECT u.user_id, u.first_name, u.last_name, f.friends_since
			FROM users u
			JOIN (
    			SELECT friends_since,
        			CASE
			            WHEN user1_id = $1 THEN user2_id
			            WHEN user2_id = $1 THEN user1_id
					END AS friend_id
				FROM friends ) as f
			ON f.friend_id = u.user_id `

	response, err := connPool.Pool.Query(ctx, query, uid)
	if err != nil {
		fmt.Fprintf(w, "Error query friends with error: %v", err)
		return
	}

	for response.Next() {
		var friend m.Friend

		response.Scan(&friend.ID, &friend.FirstName, &friend.LastName, &friend.FriendsSince)

		friends = append(friends, friend)
	}

	responseBytes, err := json.MarshalIndent(friends, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}
