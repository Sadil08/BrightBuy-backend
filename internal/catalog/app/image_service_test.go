package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
	"time"

	"brightbuy-backend/internal/catalog/domain"
)

type fakeImageStorage struct {
	objects map[string][]byte
	deleted []string
}

func (f *fakeImageStorage) PresignUpload(context.Context, string, string, time.Duration) (string, error) {
	return "https://upload.test", nil
}
func (f *fakeImageStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.objects[key])), nil
}
func (f *fakeImageStorage) Put(_ context.Context, key, _ string, _ int64, body io.Reader) error {
	payload, err := io.ReadAll(body)
	f.objects[key] = payload
	return err
}
func (f *fakeImageStorage) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	delete(f.objects, key)
	return nil
}

type fakeImageRepository struct{}

func (fakeImageRepository) ProductExists(context.Context, int64) (bool, error) { return true, nil }
func (fakeImageRepository) CreateImage(_ context.Context, image domain.Image) (domain.Image, error) {
	image.ID = 1
	return image, nil
}
func (fakeImageRepository) DeleteImage(context.Context, int64, int64) (string, error) {
	return "products/1/image", nil
}

func pngFixture(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestConfirmRejectsMismatchedMagicBytesAndDeletesObject(t *testing.T) {
	storage := &fakeImageStorage{objects: map[string][]byte{"products/1/bad": []byte("not an image")}}
	service := NewImageService(storage, fakeImageRepository{})
	_, err := service.Confirm(context.Background(), 1, "products/1/bad", "image/png")
	if err == nil || len(storage.deleted) != 1 {
		t.Fatalf("expected rejection and cleanup, err=%v deleted=%v", err, storage.deleted)
	}
}

func TestConfirmNormalizesImageBeforePersisting(t *testing.T) {
	storage := &fakeImageStorage{objects: map[string][]byte{"products/1/good": pngFixture(t)}}
	service := NewImageService(storage, fakeImageRepository{})
	created, err := service.Confirm(context.Background(), 1, "products/1/good", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if created.Width != 2 || created.Height != 3 || created.ByteSize == 0 {
		t.Fatalf("unexpected image metadata: %+v", created)
	}
}
