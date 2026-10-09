package mysql

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestImageStorage_MinIOIntegration(t *testing.T) {
	endpoint := os.Getenv("S3_ENDPOINT_URL")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9000"
	}
	accessKey := getTestEnv("S3_ACCESS_KEY", "devaccesskey")
	secretKey := getTestEnv("S3_SECRET_KEY", "devsecretkey")
	bucket := getTestEnv("S3_BUCKET", "brightbuy-images")

	storage, err := NewImageStorage(endpoint, accessKey, secretKey, bucket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	exists, err := storage.client.BucketExists(ctx, bucket)
	if err != nil {
		t.Skipf("MinIO unavailable: %v", err)
	}
	if !exists {
		if err := storage.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("create test bucket: %v", err)
		}
	}

	objectKey := "integration/image-storage-" + time.Now().UTC().Format("20060102150405.000000000")
	t.Cleanup(func() { _ = storage.Delete(context.Background(), objectKey) })

	firstPayload := []byte("first image payload")
	secondPayload := []byte("normalized image payload")
	if err := storage.Put(ctx, objectKey, "image/png", int64(len(firstPayload)), bytes.NewReader(firstPayload)); err != nil {
		t.Fatalf("Put(): %v", err)
	}
	assertObjectContents(t, storage, ctx, objectKey, firstPayload)

	presigned, err := storage.PresignUpload(ctx, objectKey, "image/png", 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignUpload(): %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, presigned, bytes.NewReader(secondPayload))
	if err != nil {
		t.Fatalf("create presigned request: %v", err)
	}
	request.Header.Set("Content-Type", "image/png")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("presigned PUT: %v", err)
	}
	if response.Body != nil {
		_ = response.Body.Close()
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("presigned PUT status = %d", response.StatusCode)
	}
	assertObjectContents(t, storage, ctx, objectKey, secondPayload)

	if err := storage.Delete(ctx, objectKey); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if _, err := storage.Open(ctx, objectKey); err == nil {
		t.Fatal("expected deleted object to be unavailable")
	}
}

func assertObjectContents(t *testing.T, storage *ImageStorage, ctx context.Context, key string, expected []byte) {
	t.Helper()
	object, err := storage.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer object.Close()
	actual, err := io.ReadAll(object)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("object contents = %q, want %q", actual, expected)
	}
}

func getTestEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
