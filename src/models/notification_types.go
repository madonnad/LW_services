package models

import "time"

type Notification struct {
	SummaryNotifications []SummaryNotification       `json:"summary_notifications"`
	AlbumRequests        []AlbumRequestNotification  `json:"album_invites"`
	FriendRequests       []FriendRequestNotification `json:"friend_requests"`
}

type GenericNotification struct {
	NotificationID   string    `json:"notification_id"`
	MediaID          string    `json:"media_id"`
	AlbumID          string    `json:"album_id"`
	AlbumName        string    `json:"album_name"`
	NotifierID       string    `json:"notifier_id"`
	NotifierName     string    `json:"notifier_name"`
	NotificationSeen bool      `json:"notification_seen"`
	NotificationType string    `json:"notification_type"`
	ReceivedAt       time.Time `json:"received_at"`
}

type AlbumRequestNotification struct {
	RequestID    string    `json:"request_id"`
	AlbumID      string    `json:"album_id"`
	AlbumName    string    `json:"album_name"`
	AlbumCoverID string    `json:"album_cover_id"`
	AlbumOwner   string    `json:"album_owner"`
	OwnerFirst   string    `json:"owner_first"`
	OwnerLast    string    `json:"owner_last"`
	ReceivedAt   time.Time `json:"received_at"`
	Status       string    `json:"status"`
	RequestSeen  bool      `json:"request_seen"`
}

type AlbumRequestResponse struct {
	AlbumID      string    `json:"album_id"`
	AlbumName    string    `json:"album_name"`
	AlbumCoverID string    `json:"album_cover_id"`
	ReceiverID   string    `json:"receiver_id"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	Accepted     bool      `json:"accepted"`
	ReceivedAt   time.Time `json:"received_at"`
}

type FriendRequestNotification struct {
	RequestID   string    `json:"request_id"`
	ReceivedAt  time.Time `json:"received_at"`
	SenderID    string    `json:"sender_id"`
	ReceiverID  string    `json:"receiver_id"`
	FirstName   string    `json:"first_name"`
	LastName    string    `json:"last_name"`
	Status      string    `json:"status"`
	RequestSeen bool      `json:"request_seen"`
}

type SummaryNotification struct {
	NotificationType string    `json:"notification_type"`
	NameOne          string    `json:"name_one"`
	NameTwo          string    `json:"name_two"`
	AlbumName        string    `json:"album_name"`
	AlbumID          string    `json:"album_id"`
	AlbumCoverID     string    `json:"album_cover_id"`
	ReceivedAt       time.Time `json:"received_at"`
	AlbumTypeTotal   int       `json:"album_type_total"`
	RequestSeen      bool      `json:"request_seen"`
}
