package models

type Guest struct {
	ID        string `json:"user_id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Accepted  bool   `json:"accepted"`
}
