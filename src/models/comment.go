package models

import (
	"github.com/jackc/pgx/v5/pgtype"
	"time"
)

type Comment struct {
	ID         string             `json:"id"`
	ImageID    string             `json:"image_id"`
	ImageOwner string             `json:"image_owner"`
	AlbumID    string             `json:"album_id"`
	AlbumName  string             `json:"album_name"`
	UserID     string             `json:"user_id"`
	FirstName  string             `json:"first_name"`
	LastName   string             `json:"last_name"`
	Comment    string             `json:"comment"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  pgtype.Timestamptz `json:"updated_at"`
	Seen       bool               `json:"seen"`
}

type NewComment struct {
	Comment string `json:"comment"`
	ImageID string `json:"image_id"`
}

type UpdateComment struct {
	ID      string `json:"id"`
	Comment string `json:"comment"`
}
