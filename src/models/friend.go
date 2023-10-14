package models

import "time"

type Friend struct {
	ID           string    `json:"user_id"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	FriendsSince time.Time `json:"friends_since"`
}
