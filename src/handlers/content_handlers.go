package handlers

import (
	"bytes"
	"cloud.google.com/go/storage"
	"context"
	"fmt"
	"github.com/disintegration/imaging"
	image2 "image"
	"io"
	m "last_weekend_services/src/models"
	"log"
	"net/http"
	"strings"
	"time"
)

func ContentEndpointHandler(ctx context.Context, connPool *m.PGPool, gcpStorage storage.Client, liveBucket string, stagingBucket string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		///log.Print(r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			switch r.URL.Path {
			case "/image":
				ServeImage(ctx, w, r, gcpStorage, liveBucket, stagingBucket)
			case "/upload":
				GenerateAndSendSignedUrl(w, r, gcpStorage, stagingBucket)
			}
		case http.MethodPost:
			switch r.URL.Path {
			case "/resize":
				ResizeAllImages(ctx, w, connPool, gcpStorage, liveBucket)

			}
		}
	})
}

func GenerateAndSendSignedUrl(w http.ResponseWriter, r *http.Request, gcpStorage storage.Client, bucket string) {
	object := r.URL.Query().Get("id")

	opts := &storage.SignedURLOptions{
		Scheme: storage.SigningSchemeV4,
		Method: http.MethodPut,
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

func ServeImage(ctx context.Context, w http.ResponseWriter, r *http.Request, gcpStorage storage.Client, liveBucket string, stagingBucket string) {
	imageId := r.URL.Query().Get("id")

	obj := gcpStorage.Bucket(liveBucket).Object(imageId)
	liveReader, err := obj.NewReader(ctx)
	if err != nil {

		// Check if the error was that the object does not exist
		if strings.Contains(err.Error(), "object doesn't exist") {
			//Strip out the resolution portion of the ID if it exists
			parts := strings.Split(imageId, "_")
			cleanUUID := parts[0]

			intObj := gcpStorage.Bucket(stagingBucket).Object(cleanUUID)
			stagingReader, internalErr := intObj.NewReader(ctx)
			if internalErr != nil {

				// Check if the error was that the object does not exist
				if strings.Contains(internalErr.Error(), "object doesn't exist") {
					var phString string
					var phObj *storage.ObjectHandle
					var phReader *storage.Reader
					var phErr error

					if len(parts) > 1 {
						resolution := parts[1]
						phString = fmt.Sprintf("LW_placeholder_%v", resolution)
					} else {
						phString = "LW_placeholder"
					}

					phObj = gcpStorage.Bucket(liveBucket).Object(phString)
					phReader, phErr = phObj.NewReader(ctx)
					if phErr != nil {
						log.Printf("No placeholder image: %v", err)
						return
					}

					phErr = SendImage(w, *phReader)
					if phErr != nil {
						log.Printf("Could not send placeholder image data %v", err)
						return
					}
					return
				}

				log.Printf("Staging Bucket Check: %v", err)
				return
			}

			internalErr = SendImage(w, *stagingReader)
			if internalErr != nil {
				log.Printf("Could not send staging image data %v", err)
				return
			}
			return
		}

		log.Printf("Live Bucket Check: %v", err)
		return
	}

	err = SendImage(w, *liveReader)
	if err != nil {
		log.Printf("Could not send live image data %v", err)
		return
	}
	return

}

func SendImage(w http.ResponseWriter, imageReader storage.Reader) error {

	var imageBytes []byte

	imageBytes, err := io.ReadAll(&imageReader)
	if err != nil {
		log.Printf("%v", err)
		return err
	}

	w.Header().Set("Content-Type", "image/jpeg")
	_, err = w.Write(imageBytes)
	if err != nil {
		log.Printf("%v", err)
		return err
	}

	imageReader.Close()

	return nil
}

func ResizeAllImages(ctx context.Context, w http.ResponseWriter, connPool *m.PGPool, gcpStorage storage.Client, bucket string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	var countCompleted int

	query := `SELECT user_id FROM users`
	queryCount := `SELECT COUNT(user_id) FROM users`

	var rowCount int
	err := connPool.Pool.QueryRow(ctx, queryCount).Scan(&rowCount)
	if err != nil {
		log.Printf("Count query error: %v", err)
		return
	}

	response, err := connPool.Pool.Query(ctx, query)
	if err != nil {
		log.Printf("%v", err)
		return
	}

	initialMessage := []byte(fmt.Sprintf("There are a total of %v images.", rowCount))
	log.Printf("There are a total of %v images.", rowCount)

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(initialMessage)
	flusher.Flush()

	for response.Next() {
		var uuid string

		err = response.Scan(&uuid)
		if err != nil {
			log.Printf("%v", err)
			return
		}

		idString := []byte(fmt.Sprint(uuid))
		log.Printf("UUID: %v", uuid)

		w.Write(idString)
		flusher.Flush()

		obj := gcpStorage.Bucket(bucket).Object(uuid)
		reader, err := obj.NewReader(ctx)
		if err != nil {
			countCompleted++
			continue
		}

		image, err := imaging.Decode(reader, imaging.AutoOrientation(true))
		if err != nil {
			log.Printf("failed to open image: %v", err)
			countCompleted++
			continue
		}

		largeImage, fileName1080 := clampImageToSize(1080, image, uuid)
		smallImage, fileName540 := clampImageToSize(540, image, uuid)

		err = encodeAndWriteToBucket(ctx, gcpStorage.Bucket(bucket), largeImage, fileName1080)
		if err != nil {
			log.Fatalf("failed to upload large image: %v", err)
		}
		err = encodeAndWriteToBucket(ctx, gcpStorage.Bucket(bucket), smallImage, fileName540)
		if err != nil {
			log.Fatalf("failed to upload small image: %v", err)
		}

		err = reader.Close()
		if err != nil {
			log.Printf("Error closing reader for object %s: %v", uuid, err)
			countCompleted++
			continue
		}

		countCompleted++

		iteratorString := []byte(fmt.Sprintf("%d of %d images have been processed", countCompleted, rowCount))

		log.Printf("%d of %d images have been processed", countCompleted, rowCount)

		w.Write(iteratorString)
		flusher.Flush()

	}
}

func clampImageToSize(size int, image image2.Image, uuid string) (resizedImage image2.Image, fileName string) {
	fileName = fmt.Sprintf("%s_%d", uuid, size)

	width := image.Bounds().Dx()
	height := image.Bounds().Dy()

	if height >= width {
		if height > size {
			resizedImage = imaging.Resize(image, 0, 1080, imaging.Lanczos)
		} else {
			resizedImage = image
		}

	} else {
		if width > size {
			resizedImage = imaging.Resize(image, 1080, 0, imaging.Lanczos)
		} else {
			resizedImage = image
		}
	}

	return resizedImage, fileName
}

func encodeAndWriteToBucket(ctx context.Context, bucket *storage.BucketHandle, image image2.Image, name string) error {
	var buf bytes.Buffer
	err := imaging.Encode(&buf, image, imaging.JPEG)
	if err != nil {
		return err
	}

	encodedBytes := buf.Bytes()

	origObjectWriter := bucket.Object(name).NewWriter(ctx)
	defer origObjectWriter.Close()

	_, err = origObjectWriter.Write(encodedBytes)
	if err != nil {
		return err
	}

	return nil
}
