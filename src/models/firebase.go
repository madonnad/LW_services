package models

type FirebaseNotification struct {
	NotificationID string `json:"notification_id"`
	ContentName    string `json:"content_name"`
	RequesterID    string `json:"requester_id"`
	RequesterName  string `json:"requester_name"`
	RecipientID    string `json:"recipient_id"`
	Type           string `json:"type"`
}
