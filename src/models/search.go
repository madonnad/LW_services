package models

type Search struct {
	ID         string  `json:"-"`
	Name       string  `json:"name"`
	Asset      string  `json:"-"`
	FirstName  string  `json:"first_name"`
	LastName   string  `json:"last_name"`
	ResultType string  `json:"type"`
	Rank       float32 `json:"-"`
}
