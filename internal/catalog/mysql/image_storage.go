package mysql

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"brightbuy-backend/internal/catalog/app"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type ImageStorage struct {
	client *minio.Client
	bucket string
}

func NewImageStorage(endpoint, accessKey, secretKey, bucket string) (*ImageStorage, error) {
	return NewImageStorageInRegion(endpoint, accessKey, secretKey, bucket, "")
}

// NewImageStorageInRegion pins the signing region. Backblaze B2 and Cloudflare R2 reject requests
// signed for the wrong region (B2: e.g. eu-central-003, R2: auto), and the client's own region
// auto-discovery is not reliable against them. Empty region keeps the client's default behaviour.
func NewImageStorageInRegion(endpoint, accessKey, secretKey, bucket, region string) (*ImageStorage, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("image storage: invalid endpoint")
	}
	client, err := minio.New(parsed.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: strings.EqualFold(parsed.Scheme, "https"),
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("image storage: create client: %w", err)
	}
	return &ImageStorage{client: client, bucket: bucket}, nil
}

func (s *ImageStorage) PresignUpload(ctx context.Context, objectKey, contentType string, expiry time.Duration) (string, error) {
	presigned, err := s.client.PresignedPutObject(ctx, s.bucket, objectKey, expiry)
	if err != nil {
		return "", fmt.Errorf("image storage: presign upload: %w", err)
	}
	return presigned.String(), nil
}

func (s *ImageStorage) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("image storage: open object: %w", err)
	}
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		return nil, fmt.Errorf("image storage: stat object: %w", err)
	}
	return object, nil
}

func (s *ImageStorage) Put(ctx context.Context, objectKey, contentType string, size int64, body io.Reader) error {
	_, err := s.client.PutObject(ctx, s.bucket, objectKey, body, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("image storage: put object: %w", err)
	}
	return nil
}

func (s *ImageStorage) Delete(ctx context.Context, objectKey string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("image storage: delete object: %w", err)
	}
	return nil
}

var _ app.ImageStorage = (*ImageStorage)(nil)
