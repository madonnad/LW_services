package models

type GenericNotification struct {
	Notifier         interface{} `json:"notifier"`
	MediaID          string      `json:"media_id"`
	AlbumName        string      `json:"album_name"`
	NotificationSeen bool        `json:"notification_seen"`
	NotificationType string      `json:"notification_type"`
}
