package models

import (
	"github.com/jackc/pgx/v5/pgtype"
	"time"
)

type Image struct {
	ID           string        `json:"image_id"`
	ImageOwner   string        `json:"image_owner"`
	FirstName    string        `json:"first_name"`
	LastName     string        `json:"last_name"`
	Caption      string        `json:"caption"`
	Upvotes      uint          `json:"upvotes"`
	UpvotedUsers []pgtype.UUID `json:"upvoted_users"`
	LikedUsers   []pgtype.UUID `json:"liked_users"`
	CreatedAt    time.Time     `json:"created_at"`
}
