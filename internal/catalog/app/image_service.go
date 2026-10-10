package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/jpeg"
	"image/png"
	"io"
	"strings"
	"time"

	"brightbuy-backend/internal/catalog/domain"
	"golang.org/x/image/webp"
)

var ErrInvalidImage = errors.New("invalid image upload")

const maxImageBytes = 10 << 20

type ImageRepository interface {
	ProductExists(context.Context, int64) (bool, error)
	CreateImage(context.Context, domain.Image) (domain.Image, error)
	DeleteImage(context.Context, int64, int64) (string, error)
}

// PresignTTL is how long a presigned upload URL stays valid (openapi: expires in 5 minutes).
const PresignTTL = 5 * time.Minute

type UploadURL struct {
	ObjectKey string `json:"object_key"`
	URL       string `json:"upload_url"`
}

type ImageService struct {
	storage ImageStorage
	repo    ImageRepository
	clock   func() time.Time
}

func NewImageService(storage ImageStorage, repo ImageRepository) *ImageService {
	return &ImageService{storage: storage, repo: repo, clock: time.Now}
}

func (s *ImageService) PresignUpload(ctx context.Context, productID int64, contentType string) (UploadURL, error) {
	if productID <= 0 || !supportedContentType(contentType) {
		return UploadURL{}, fmt.Errorf("%w: unsupported product or content type", domain.ErrValidation)
	}
	exists, err := s.repo.ProductExists(ctx, productID)
	if err != nil {
		return UploadURL{}, err
	}
	if !exists {
		return UploadURL{}, domain.ErrNotFound
	}
	key, err := newObjectKey(productID)
	if err != nil {
		return UploadURL{}, err
	}
	url, err := s.storage.PresignUpload(ctx, key, contentType, PresignTTL)
	if err != nil {
		return UploadURL{}, err
	}
	return UploadURL{ObjectKey: key, URL: url}, nil
}

func (s *ImageService) Confirm(ctx context.Context, productID int64, objectKey, declaredContentType string) (domain.Image, error) {
	if productID <= 0 || !validObjectKey(productID, objectKey) || !supportedContentType(declaredContentType) {
		return domain.Image{}, fmt.Errorf("%w: upload metadata is invalid", domain.ErrValidation)
	}
	object, err := s.storage.Open(ctx, objectKey)
	if err != nil {
		return domain.Image{}, err
	}
	defer object.Close()

	payload, err := io.ReadAll(io.LimitReader(object, maxImageBytes+1))
	if err != nil {
		return domain.Image{}, err
	}
	if int64(len(payload)) == 0 || int64(len(payload)) > maxImageBytes {
		return s.reject(ctx, objectKey, "image size is invalid")
	}
	actualType := sniffContentType(payload)
	if actualType == "" || actualType != declaredContentType {
		return s.reject(ctx, objectKey, "image magic bytes do not match content type")
	}

	decoded, normalizedType, err := decodeImage(payload, actualType)
	if err != nil {
		return s.reject(ctx, objectKey, "image cannot be decoded")
	}
	clean, err := encodeWithoutMetadata(decoded, normalizedType)
	if err != nil {
		return s.reject(ctx, objectKey, "image cannot be normalized")
	}
	if err := s.storage.Put(ctx, objectKey, normalizedType, int64(len(clean)), bytes.NewReader(clean)); err != nil {
		return domain.Image{}, err
	}

	stored := domain.Image{ProductID: productID, ObjectKey: objectKey, ContentType: normalizedType, ByteSize: int64(len(clean)), Width: decoded.Bounds().Dx(), Height: decoded.Bounds().Dy()}
	created, err := s.repo.CreateImage(ctx, stored)
	if err != nil {
		_ = s.storage.Delete(ctx, objectKey)
		return domain.Image{}, err
	}
	return created, nil
}

func (s *ImageService) Delete(ctx context.Context, productID, imageID int64) error {
	if productID <= 0 || imageID <= 0 {
		return fmt.Errorf("%w: image IDs must be positive", domain.ErrValidation)
	}
	objectKey, err := s.repo.DeleteImage(ctx, productID, imageID)
	if err != nil {
		return err
	}
	return s.storage.Delete(ctx, objectKey)
}

func (s *ImageService) reject(ctx context.Context, objectKey, reason string) (domain.Image, error) {
	_ = s.storage.Delete(ctx, objectKey)
	return domain.Image{}, fmt.Errorf("%w: %s", ErrInvalidImage, reason)
}

func supportedContentType(contentType string) bool {
	return contentType == "image/jpeg" || contentType == "image/png" || contentType == "image/webp"
}

func sniffContentType(payload []byte) string {
	if len(payload) >= 3 && payload[0] == 0xff && payload[1] == 0xd8 && payload[2] == 0xff {
		return "image/jpeg"
	}
	if len(payload) >= 8 && bytes.Equal(payload[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
		return "image/png"
	}
	if len(payload) >= 12 && string(payload[:4]) == "RIFF" && string(payload[8:12]) == "WEBP" {
		return "image/webp"
	}
	return ""
}

func encodeWithoutMetadata(decoded image.Image, contentType string) ([]byte, error) {
	var output bytes.Buffer
	switch contentType {
	case "image/jpeg":
		err := jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 90})
		return output.Bytes(), err
	case "image/png":
		err := png.Encode(&output, decoded)
		return output.Bytes(), err
	default:
		return nil, fmt.Errorf("unsupported image type %q", contentType)
	}
}

func decodeImage(payload []byte, contentType string) (image.Image, string, error) {
	if contentType == "image/webp" {
		decoded, err := webp.Decode(bytes.NewReader(payload))
		return decoded, "image/png", err
	}
	decoded, _, err := image.Decode(bytes.NewReader(payload))
	return decoded, contentType, err
}

func validObjectKey(productID int64, objectKey string) bool {
	return strings.HasPrefix(objectKey, fmt.Sprintf("products/%d/", productID)) && len(objectKey) > len(fmt.Sprintf("products/%d/", productID))
}

func newObjectKey(productID int64) (string, error) {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("create image key: %w", err)
	}
	return fmt.Sprintf("products/%d/%s", productID, hex.EncodeToString(randomBytes)), nil
}

// ImageLookup is the optional repository capability behind serving images through the API. It is a
// separate interface so storage backends/stubs that only upload don't have to implement it.
type ImageLookup interface {
	GetImage(context.Context, int64) (domain.Image, error)
}

// Open streams one stored image. This is what makes a PRIVATE bucket work: the bucket is never exposed,
// the API reads the object with its own credentials and the frontend caches the result.
func (s *ImageService) Open(ctx context.Context, imageID int64) (io.ReadCloser, string, error) {
	lookup, ok := s.repo.(ImageLookup)
	if !ok || imageID <= 0 {
		return nil, "", domain.ErrNotFound
	}
	img, err := lookup.GetImage(ctx, imageID)
	if err != nil {
		return nil, "", err
	}
	body, err := s.storage.Open(ctx, img.ObjectKey)
	if err != nil {
		return nil, "", err
	}
	return body, img.ContentType, nil
}
