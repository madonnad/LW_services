package models

import "time"

type Notification struct {
	EngagementNotification []EngagementNotification    `json:"engagement_notification"`
	AlbumRequests          []AlbumRequestNotification  `json:"album_invites"`
	FriendRequests         []FriendRequestNotification `json:"friend_requests"`
	AlbumRequestResponses  []AlbumRequestNotification  `json:"album_request_responses"`
	CommentNotifications   []Comment                   `json:"comment_notifications"`
}

type EngagementNotification struct {
	NotificationID   string    `json:"notification_id"` // Notification UUID
	ImageID          string    `json:"image_id"`        // Image_ID
	AlbumID          string    `json:"album_id"`
	AlbumName        string    `json:"album_name"`
	ReceiverID       string    `json:"receiver_id"` // Content Owner
	NotifierID       string    `json:"notifier_id"` // Person who is engaging
	NotifierFirst    string    `json:"notifier_first"`
	NotifierLast     string    `json:"notifier_last"`
	NotificationSeen bool      `json:"notification_seen"` // Seen by the owner of that content (We don't care about album guests receiving noti's they just need to get their albums updated)
	NotificationType string    `json:"notification_type"` // UPVOTE, LIKE, COMMENT, etc etc
	NewCount         int       `json:"new_count"`
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
	GuestID      string    `json:"guest_id"`
	GuestFirst   string    `json:"guest_first"`
	GuestLast    string    `json:"guest_last"`
	Status       string    `json:"status"`
	InviteSeen   bool      `json:"invite_seen"`
	ResponseSeen bool      `json:"response_seen"`
	ReceivedAt   time.Time `json:"received_at"`
	UnlockedAt   time.Time `json:"unlocked_at"`
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
