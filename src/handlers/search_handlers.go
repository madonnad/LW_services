package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	m "last_weekend_services/src/models"
	"log"
	"net/http"
	"strings"

	"github.com/opensearch-project/opensearch-go"
	"github.com/opensearch-project/opensearch-go/opensearchapi"
	/*jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/redis/go-redis/v9"*/)

func SearchEndpointHandler(ctx context.Context, connPool *m.PGPool, openSearchClient *opensearch.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		/*claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
			}*/

		switch r.Method {
		case http.MethodGet:
			searchVal := r.URL.Query().Get("lookup")
			AlbumFriendTextSearch(ctx, w, connPool, openSearchClient, searchVal)
		}
	})
}

func AlbumFriendTextSearch(ctx context.Context, w http.ResponseWriter, connPool *m.PGPool, openSearchClient *opensearch.Client, searchVal string) {
	var results []m.Search
	var openSearchResults m.SearchResults

	size := 10
	const IndexName = "global-search"

	queryData := map[string]interface{}{
		"size": size,
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":    searchVal,
				"fields":   []string{"name^4", "first_name", "last_name"},
				"type":     "bool_prefix",
				"analyzer": "simple",
			},
		},
	}

	jsonData, err := json.Marshal(queryData)
	if err != nil {
		panic(err)
	}

	content := strings.NewReader(string(jsonData))

	search := opensearchapi.SearchRequest{
		Index:  []string{IndexName},
		Body:   content,
		Pretty: true,
	}

	searchResponse, err := search.Do(ctx, openSearchClient)
	if err != nil {
		fmt.Fprintf(w, "Failed to lookup search: %v", err)
	}

	bytes, err := io.ReadAll(searchResponse.Body)
	if err != nil {
		fmt.Fprintf(w, "Failed with: %v", err)
	}

	err = json.Unmarshal(bytes, &openSearchResults)
	if err != nil {
		fmt.Fprintf(w, "Unable to parse JSON: %v", err)
	}

	//log.Printf("Result: %v", openSearchResults.Hits.Hits)

	for _, value := range openSearchResults.Hits.Hits {
		var result m.Search
		result.ID = value.ID
		result.Score = value.Score
		result.Name = value.Source.Name
		result.FirstName = value.Source.FirstName
		result.LastName = value.Source.LastName
		result.ResultType = value.Source.ResultType

		switch value.Source.ResultType {
		case "user":
			result.Asset = value.ID
		case "album":
			queryCover := `SELECT album_cover_id FROM albums WHERE album_id=$1`

			row := connPool.Pool.QueryRow(ctx, queryCover, value.ID)
			err := row.Scan(&result.Asset)
			if err != nil {
				fmt.Fprintf(w, "Unable to find AlbumCoverID: %v", err)
				result.Asset = ""
			}
		}
		results = append(results, result)

	}

	responseBytes, err := json.MarshalIndent(results, "", "\t")
	if err != nil {
		log.Panic(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}
