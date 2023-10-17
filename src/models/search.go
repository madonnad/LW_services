package models

type Search struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Asset      string  `json:"asset"`
	FirstName  string  `json:"first_name"`
	LastName   string  `json:"last_name"`
	ResultType string  `json:"type"`
	Score      float64 `json:"score"`
}

type SearchResults struct {
	MaxScore float64 `json:"max_score"`
	Hits     Hits    `json:"hits"`
}

type Hits struct {
	Hits []Hit `json:"hits"`
}

type Hit struct {
	Index  string  `json:"_index"`
	ID     string  `json:"_id"`
	Score  float64 `json:"_score"`
	Source Source  `json:"_source"`
}

type Source struct {
	Name       string `json:"name"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	ResultType string `json:"type"`
}
