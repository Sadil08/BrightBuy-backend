package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/catalog/domain"
)

type ImageRepository struct {
	db *sql.DB
}

func NewImageRepository(db *sql.DB) *ImageRepository {
	return &ImageRepository{db: db}
}

func (r *ImageRepository) ProductExists(ctx context.Context, productID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM product WHERE product_id = ? AND is_active = TRUE)`, productID).Scan(&exists)
	return exists, err
}

func (r *ImageRepository) CreateImage(ctx context.Context, image domain.Image) (domain.Image, error) {
	result, err := r.db.ExecContext(ctx, `INSERT INTO product_image (product_id, object_key, content_type, byte_size, width, height) VALUES (?, ?, ?, ?, ?, ?)`, image.ProductID, image.ObjectKey, image.ContentType, image.ByteSize, image.Width, image.Height)
	if err != nil {
		return domain.Image{}, err
	}
	image.ID, err = result.LastInsertId()
	return image, err
}

func (r *ImageRepository) DeleteImage(ctx context.Context, productID, imageID int64) (string, error) {
	var objectKey string
	if err := r.db.QueryRowContext(ctx, `SELECT object_key FROM product_image WHERE image_id = ? AND product_id = ?`, imageID, productID).Scan(&objectKey); err != nil {
		if err == sql.ErrNoRows {
			return "", domain.ErrNotFound
		}
		return "", err
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM product_image WHERE image_id = ? AND product_id = ?`, imageID, productID); err != nil {
		return "", fmt.Errorf("delete image row: %w", err)
	}
	return objectKey, nil
}

var _ app.ImageRepository = (*ImageRepository)(nil)

// GetImage returns one stored image's metadata (used to serve it through the API when the bucket is private).
func (r *ImageRepository) GetImage(ctx context.Context, imageID int64) (domain.Image, error) {
	var img domain.Image
	err := r.db.QueryRowContext(ctx, `SELECT image_id, product_id, object_key, content_type FROM product_image WHERE image_id = ?`, imageID).
		Scan(&img.ID, &img.ProductID, &img.ObjectKey, &img.ContentType)
	if err == sql.ErrNoRows {
		return domain.Image{}, domain.ErrNotFound
	}
	return img, err
}

var _ app.ImageLookup = (*ImageRepository)(nil)
