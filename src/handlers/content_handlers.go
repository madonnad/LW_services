package handlers

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/storage"
)

func ContentEndpointHandler(ctx context.Context, gcpStorage storage.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		///log.Print(r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			switch r.URL.Path {
			case "/image":
				ServeImage(ctx, w, r, gcpStorage)
			case "/upload":
				GenerateAndSendSignedUrl(ctx, w, r, gcpStorage)
			}
		}
	})
}

func GenerateAndSendSignedUrl(ctx context.Context, w http.ResponseWriter, r *http.Request, gcpStorage storage.Client) {
	object := r.URL.Query().Get("id")

	opts := &storage.SignedURLOptions{
		Scheme: storage.SigningSchemeV4,
		Method: "PUT",
		Headers: []string{
			"Content-Type:application/octet-stream",
		},
		Expires: time.Now().UTC().Add(3 * time.Minute),
	}

	url, err := gcpStorage.Bucket("lw-user-images").SignedURL(object, opts)
	if err != nil {
		log.Printf("Unable to generate signed URL for upload link: %v", err)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(url))
}

func ServeImage(ctx context.Context, w http.ResponseWriter, r *http.Request, gcpStorage storage.Client) {
	imageId := r.URL.Query().Get("id")

	obj := gcpStorage.Bucket("lw-user-images").Object(imageId)
	imageReader, err := obj.NewReader(ctx)
	if err != nil {
		log.Printf("%v", err)
		return
	}

	imageBytes, err := io.ReadAll(imageReader)
	if err != nil {
		log.Printf("%v", err)
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(imageBytes)
}
