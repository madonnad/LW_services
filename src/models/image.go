package models

import (
	"time"
)

type Image struct {
	ID          string    `json:"image_id"`
	ImageOwner  string    `json:"image_owner"`
	FirstName   string    `json:"first_name"`
	LastName    string    `json:"last_name"`
	Caption     string    `json:"caption"`
	Upvotes     uint      `json:"upvotes"`
	Likes       uint      `json:"likes"`
	UserUpvoted bool      `json:"user_upvoted"`
	UserLiked   bool      `json:"user_liked"`
	CreatedAt   time.Time `json:"created_at"`
}
