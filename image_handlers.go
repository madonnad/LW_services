package main

import "time"

type Image struct {
	ID         string    `json:"image_id"`
	ImageOwner string    `json:"image_owner"`
	Caption    string    `json:"caption"`
	Upvotes    uint      `json:"upvotes"`
	CreatedAt  time.Time `json:"created_at"`
}
