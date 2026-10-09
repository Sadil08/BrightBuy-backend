package domain

import "errors"

var (
	ErrNotFound         = errors.New("catalog resource not found")
	ErrValidation       = errors.New("catalog validation failed")
	ErrSKUConflict      = errors.New("variant SKU already exists")
	ErrCategoryInactive = errors.New("category is missing or inactive")
)

type Image struct {
	ID          int64  `json:"id"`
	ProductID   int64  `json:"product_id"`
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}
