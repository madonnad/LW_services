package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	m "last_weekend_services/src/models"
	"log"
	"net/http"
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

	searchVal = searchVal + ":*"
	//print(searchVal)

	query :=
		`SELECT id, lookup, asset, first, last, type, ts_rank(to_tsvector('simple', search_table.lookup), query) as rank_search
			FROM
			(SELECT u.user_id AS id,
					u.first_name || ' ' || u.last_name AS lookup,
					u.first_name AS first,
					u.last_name AS last,
					u.user_id AS asset,
					'user' AS type
			FROM users u
			UNION
			SELECT  a.album_id AS id,
			        a.album_name AS lookup,
			        u.first_name AS first,
			        u.last_name AS last,
					a.album_cover_id AS asset,
			        'album' AS type
			FROM albums a
			INNER JOIN users u ON a.album_owner = u.user_id) AS search_table,
			    to_tsvector('simple',search_table.lookup) document,
			    to_tsquery('simple',$1) query
			WHERE query @@ document
			ORDER BY rank_search DESC;`

	response, err := connPool.Pool.Query(ctx, query, searchVal)
	if err != nil {
		fmt.Fprintf(w, "Error query search with error: %v", err)
		return
	}

	for response.Next() {
		var result m.Search

		err := response.Scan(&result.ID, &result.Name, &result.Asset, &result.FirstName, &result.LastName, &result.ResultType, &result.Rank)
		if err != nil {
			fmt.Fprintf(w, "Error parsing search into object with error: %v", err)
			return
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
