package models

type Search struct {
	ID         string  `json:"id"`
	Lookup     string  `json:"lookup"`
	Asset      string  `json:"asset"`
	FirstName  string  `json:"first_name"`
	LastName   string  `json:"last_name"`
	ResultType string  `json:"result_type"`
	Rank       float32 `json:"rank"`
}
