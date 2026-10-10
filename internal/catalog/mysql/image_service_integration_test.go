package mysql

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"testing"
	"time"

	"brightbuy-backend/internal/catalog/app"

	"github.com/minio/minio-go/v7"
)

func TestImageService_MinIOAndMySQLIntegration(t *testing.T) {
	db := repoTestDB(t)
	storage := testImageStorage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	repository := NewImageRepository(db)
	service := app.NewImageService(storage, repository)
	catalogRepository := NewRepository(db)
	product, err := catalogRepository.CreateProduct(ctx, app.AdminProduct{
		Name:   uniqueValue("Image-Integration-Product"),
		Active: true,
	}, nil, nil)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	t.Cleanup(func() { cleanupProduct(t, db, product.ID) })

	upload, err := service.PresignUpload(ctx, product.ID, "image/png")
	if err != nil {
		t.Fatalf("PresignUpload(): %v", err)
	}
	payload := pngIntegrationFixture(t)
	putPresignedObject(t, ctx, upload.URL, "image/png", payload)

	created, err := service.Confirm(ctx, product.ID, upload.ObjectKey, "image/png")
	if err != nil {
		t.Fatalf("Confirm(): %v", err)
	}
	if created.ID == 0 || created.ProductID != product.ID || created.Width != 2 || created.Height != 3 {
		t.Fatalf("unexpected confirmed image: %+v", created)
	}

	var rowCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_image WHERE image_id = ?`, created.ID).Scan(&rowCount); err != nil {
		t.Fatalf("query confirmed image row: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("image row count = %d, want 1", rowCount)
	}
	assertImageObjectIsReadable(t, storage, ctx, upload.ObjectKey)

	if err := service.Delete(ctx, product.ID, created.ID); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_image WHERE image_id = ?`, created.ID).Scan(&rowCount); err != nil {
		t.Fatalf("query deleted image row: %v", err)
	}
	if rowCount != 0 {
		t.Fatalf("image row count after delete = %d, want 0", rowCount)
	}
	if _, err := storage.Open(ctx, upload.ObjectKey); err == nil {
		t.Fatal("expected confirmed image object to be deleted")
	}
}

func TestImageService_InvalidUploadIsDeleted(t *testing.T) {
	db := repoTestDB(t)
	storage := testImageStorage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	product, err := NewRepository(db).CreateProduct(ctx, app.AdminProduct{
		Name:   uniqueValue("Invalid-Image-Product"),
		Active: true,
	}, nil, nil)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	t.Cleanup(func() { cleanupProduct(t, db, product.ID) })

	service := app.NewImageService(storage, NewImageRepository(db))
	upload, err := service.PresignUpload(ctx, product.ID, "image/png")
	if err != nil {
		t.Fatalf("PresignUpload(): %v", err)
	}
	putPresignedObject(t, ctx, upload.URL, "image/png", []byte("not an image"))

	_, err = service.Confirm(ctx, product.ID, upload.ObjectKey, "image/png")
	if !errors.Is(err, app.ErrInvalidImage) {
		t.Fatalf("Confirm() error = %v, want invalid image", err)
	}
	if _, err := storage.Open(ctx, upload.ObjectKey); err == nil {
		t.Fatal("expected invalid upload object to be deleted")
	}
}

func testImageStorage(t *testing.T) *ImageStorage {
	t.Helper()
	endpoint := getTestEnv("S3_ENDPOINT_URL", "http://127.0.0.1:9000")
	storage, err := NewImageStorage(endpoint, getTestEnv("S3_ACCESS_KEY", "devaccesskey"), getTestEnv("S3_SECRET_KEY", "devsecretkey"), getTestEnv("S3_BUCKET", "brightbuy-images"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exists, err := storage.client.BucketExists(ctx, storage.bucket)
	if err != nil {
		t.Skipf("MinIO unavailable: %v", err)
	}
	if !exists {
		if err := storage.client.MakeBucket(ctx, storage.bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("create image bucket: %v", err)
		}
	}
	return storage
}

func putPresignedObject(t *testing.T, ctx context.Context, uploadURL, contentType string, payload []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("create upload request: %v", err)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("presigned upload: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("presigned upload status = %d", response.StatusCode)
	}
}

func assertImageObjectIsReadable(t *testing.T, storage *ImageStorage, ctx context.Context, objectKey string) {
	t.Helper()
	object, err := storage.Open(ctx, objectKey)
	if err != nil {
		t.Fatalf("open confirmed image: %v", err)
	}
	defer object.Close()
	if _, err := io.ReadAll(object); err != nil {
		t.Fatalf("read confirmed image: %v", err)
	}
}

func pngIntegrationFixture(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	fixture := image.NewRGBA(image.Rect(0, 0, 2, 3))
	fixture.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&output, fixture); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
