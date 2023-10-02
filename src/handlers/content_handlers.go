package handlers

import (
	"context"
	"io"
	"log"
	"net/http"

	"cloud.google.com/go/storage"
)

func ContentEndpointHandler(ctx context.Context, gcpStorage storage.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ServeImage(ctx, w, r, gcpStorage)
		}
	})
}

func ServeImage(ctx context.Context, w http.ResponseWriter, r *http.Request, gcpStorage storage.Client) {
	imageId := r.URL.Query().Get("id")

	obj := gcpStorage.Bucket("lw-user-images").Object(imageId)
	imageReader, err := obj.NewReader(ctx)
	if err != nil {
		log.Printf("%v", err)
	}

	imageBytes, err := io.ReadAll(imageReader)
	if err != nil {
		log.Printf("%v", err)
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(imageBytes)
}
