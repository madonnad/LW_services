package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	m "last_weekend_services/src/models"
	"log"
	"net/http"
	"regexp"
	/*jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/redis/go-redis/v9"*/)

func SearchEndpointHandler(ctx context.Context, connPool *m.PGPool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		/*claims, ok := r.Context().Value(jwtmiddleware.ContextKey{}).(*validator.ValidatedClaims)
		if !ok {
			log.Printf("Failed to get validated claims")
			return
			}*/

		switch r.Method {
		case http.MethodGet:
			searchVal := r.URL.Query().Get("lookup")
			AlbumFriendTextSearch(ctx, w, connPool, searchVal)
		}
	})
}

func AlbumFriendTextSearch(ctx context.Context, w http.ResponseWriter, connPool *m.PGPool, searchVal string) {
	var results []m.Search

	queryData := `SELECT user_id, first_name, last_name, ts_rank(users.tsv_fullname, 
							to_tsquery($1)) + ts_rank(users.tsv_email,to_tsquery($1)) as rank
					FROM users
					WHERE setweight(users.tsv_fullname, 'A') || setweight(users.tsv_email, 'B') @@ to_tsquery($1)`

	// Prepare string for the search
	pattern := regexp.MustCompile(`\s+$`)
	searchString := pattern.ReplaceAllString(searchVal, ":* & ")
	searchString += ":*"

	rows, err := connPool.Pool.Query(ctx, queryData, searchString)
	if err != nil {
		fmt.Fprintf(w, "Failed to perform search: %v", err)
	}

	// Scan through the rows of search results
	for rows.Next() {
		var result m.Search

		err = rows.Scan(&result.ID, &result.FirstName, &result.LastName, &result.Score)
		if err != nil {
			fmt.Fprintf(w, "Failed to scan search: %v", err)
		}
		result.Name = result.FirstName + result.LastName
		result.Asset = result.ID
		result.ResultType = "user"

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
