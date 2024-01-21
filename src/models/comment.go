package models

import (
	"github.com/jackc/pgx/v5/pgtype"
	"time"
)

type Comment struct {
	ID        string             `json:"id"`
	ImageID   string             `json:"image_id"`
	UserID    string             `json:"user_id"`
	FirstName string             `json:"first_name"`
	LastName  string             `json:"last_name"`
	Comment   string             `json:"comment"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt pgtype.Timestamptz `json:"updated_at"`
}

type NewComment struct {
	Comment string `json:"comment"`
	ImageID string `json:"image_id"`
}

type UpdateComment struct {
	ID      string `json:"id"`
	Comment string `json:"comment"`
}
