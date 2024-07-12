package handlers

import (
	"bytes"
	"cloud.google.com/go/storage"
	"context"
	"github.com/disintegration/imaging"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

func ContentEndpointHandler(ctx context.Context, gcpStorage storage.Client, bucket string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		///log.Print(r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			switch r.URL.Path {
			case "/image":
				ServeImage(ctx, w, r, gcpStorage, bucket)
			case "/upload":
				GenerateAndSendSignedUrl(w, r, gcpStorage, bucket)
			}
		}
	})
}

func GenerateAndSendSignedUrl(w http.ResponseWriter, r *http.Request, gcpStorage storage.Client, bucket string) {
	object := r.URL.Query().Get("id")

	opts := &storage.SignedURLOptions{
		Scheme: storage.SigningSchemeV4,
		Method: "PUT",
		Headers: []string{
			"Content-Type:application/octet-stream",
		},
		Expires: time.Now().UTC().Add(3 * time.Minute),
	}

	url, err := gcpStorage.Bucket(bucket).SignedURL(object, opts)
	if err != nil {
		log.Printf("Unable to generate signed URL for upload link: %v", err)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(url))
}

func ServeImage(ctx context.Context, w http.ResponseWriter, r *http.Request, gcpStorage storage.Client, bucket string) {
	start := time.Now()
	imageId := r.URL.Query().Get("id")
	imageHeight := r.URL.Query().Get("height")

	obj := gcpStorage.Bucket(bucket).Object(imageId)
	imageReader, err := obj.NewReader(ctx)
	if err != nil {
		log.Printf("%v", err)
		return
	}
	defer imageReader.Close()

	var imageBytes []byte

	if imageHeight != "" {
		imageHeightInt, err := strconv.Atoi(imageHeight)
		if err != nil {
			log.Printf("Could not convert image height to int: %v", err)
		}

		image, err := imaging.Decode(imageReader)
		if err != nil {
			log.Printf("Could not convert image to image package struct: %v", err)
		}

		resizedImage := imaging.Resize(image, 0, imageHeightInt, imaging.Lanczos)

		var buf bytes.Buffer
		err = imaging.Encode(&buf, resizedImage, imaging.JPEG)
		if err != nil {
			log.Printf("Could not encode resized image: %v", err)
			http.Error(w, "Could not encode image", http.StatusInternalServerError)
			return
		}

		imageBytes = buf.Bytes()
	} else {
		imageBytes, err = io.ReadAll(imageReader)
		if err != nil {
			log.Printf("%v", err)
		}
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(imageBytes)
	log.Printf("height: %v duration: %v", imageHeight, time.Since(start))
}
