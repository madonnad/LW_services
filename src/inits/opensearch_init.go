package inits

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	m "last_weekend_services/src/models"

	"github.com/opensearch-project/opensearch-go"
	"github.com/opensearch-project/opensearch-go/opensearchapi"
)

func InitOpenSearch(ctx context.Context, connPool *m.PGPool, client *opensearch.Client) {
	settings := strings.NewReader(`{'settings': {'index': {'number_of_shards': 1,'number_of_replicas': 1 }}}`)

	index := "global-search"

	res := opensearchapi.IndicesCreateRequest{Index: index, Body: settings}
	fmt.Println("Creating index")
	fmt.Println(res)

	query := `SELECT id, name, first, last, type
			FROM
			(SELECT u.user_id AS id,
			       u.first_name || ' ' ||u.last_name as name,
			       u.first_name AS first,
			        u.last_name AS last,
			       'user' as type
			FROM users u
			UNION
			SELECT  a.album_id  AS id,
			        a.album_name AS lookup,
			        u.first_name AS first,
			        u.last_name AS last,
			        'album' as type
			FROM albums a
			INNER JOIN users u ON a.album_owner = u.user_id) as search`
	response, err := connPool.Pool.Query(ctx, query)
	if err != nil {
		log.Printf("Error query search with error: %v", err)
		return
	}

	for response.Next() {
		var result m.Search

		err := response.Scan(&result.ID, &result.Name, &result.FirstName, &result.LastName, &result.ResultType)
		if err != nil {
			log.Printf("Error parsing search into object with error: %v", err)
			return
		}

		data, err := json.MarshalIndent(result, "", "\t")
		document := strings.NewReader(string(data))

		req := opensearchapi.IndexRequest{
			Index:      index,
			DocumentID: result.ID,
			Body:       document,
		}
		insertResponse, err := req.Do(ctx, client)
		if err != nil {
			fmt.Println("failed to insert document ", err)
			os.Exit(1)
		}
		defer insertResponse.Body.Close()

	}
}
