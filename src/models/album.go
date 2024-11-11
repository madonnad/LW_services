package models

import (
	"errors"
	"time"
)

type Album struct {
	AlbumID      string    `json:"album_id"`
	AlbumName    string    `json:"album_name"`
	AlbumOwner   string    `json:"album_owner"`
	OwnerFirst   string    `json:"owner_first"`
	OwnerLast    string    `json:"owner_last"`
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

func (album *Album) PhaseCalculation() error {
	currentUtcTime := time.Now().UTC()

	switch {
	//case currentUtcTime.Before(album.UnlockedAt):
	//	album.Phase = "invite"
	//	return nil
	case currentUtcTime.Before(album.RevealedAt):
		album.Phase = "unlock"
		return nil
	//case currentUtcTime.Before(album.RevealedAt):
	//	album.Phase = "lock"
	//	return nil
	case currentUtcTime.After(album.RevealedAt):
		album.Phase = "reveal"
		return nil
	}

	return errors.New("error reading date and setting phase")
}
