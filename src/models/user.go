package models

import "time"

type User struct {
	ID        string    `json:"user_id"`
	Email     string    `json:"email"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	CreatedAt time.Time `json:"created_at"`
}

type SearchedUser struct {
	ID           string   `json:"user_id"`
	FirstName    string   `json:"first_name"`
	LastName     string   `json:"last_name"`
	FriendStatus string   `json:"friend_status"`
	AlbumIDs     []string `json:"album_id"`
	FriendCount  int      `json:"friend_count"`
}
