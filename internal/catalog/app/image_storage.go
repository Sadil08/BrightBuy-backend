package app

import (
	"context"
	"io"
	"time"
)

type ImageStorage interface {
	PresignUpload(context.Context, string, string, time.Duration) (string, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Put(context.Context, string, string, int64, io.Reader) error
	Delete(context.Context, string) error
}
