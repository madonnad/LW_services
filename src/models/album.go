package models

import "time"

type Album struct {
	AlbumID      string    `json:"album_id"`
	AlbumName    string    `json:"album_name"`
	AlbumOwner   string    `json:"album_owner"`
	AlbumCoverID string    `json:"album_cover_id"`
	CreatedAt    time.Time `json:"created_at"`
	LockedAt     time.Time `json:"locked_at"`
	UnlockedAt   time.Time `json:"unlocked_at"`
	RevealedAt   time.Time `json:"revealed_at"`
	Visibility   string    `json:"visibility"`
	InviteList   []Guest   `json:"invite_list"`
	Images       []Image   `json:"images"`
	Phase        string    `json:"phase"`
}
