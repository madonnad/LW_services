package models

import (
	"time"
)

type Image struct {
	ID           string    `json:"image_id"`
	ImageOwner   string    `json:"image_owner"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	Caption      string    `json:"caption"`
	Upvotes      uint      `json:"upvotes"`
	UpvotedUsers []User    `json:"upvoted_users"`
	LikedUsers   []User    `json:"liked_users"`
	CreatedAt    time.Time `json:"created_at"`
}
