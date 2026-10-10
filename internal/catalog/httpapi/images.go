package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/shared/httpx"
)

type ImageHandler struct {
	baseURL string
	service *app.ImageService
}

func NewImageHandler(service *app.ImageService) *ImageHandler {
	return &ImageHandler{service: service}
}

// WithBaseURL sets the public base the stored object keys are served from (same value as the catalog's).
func (h *ImageHandler) WithBaseURL(base string) *ImageHandler {
	h.baseURL = strings.TrimRight(base, "/")
	return h
}

// RegisterImageRoutes mounts the image routes; guard authenticates and checks `catalog:image:write`
// (distinct from catalog:write — AC-ADMINCATALOG-5).
func RegisterImageRoutes(r chi.Router, handler *ImageHandler, guard func(http.Handler) http.Handler) {
	r.Group(func(r chi.Router) {
		r.Use(guard)
		r.Post("/api/v1/staff/products/{productID}/images/upload-url", handler.presignUpload)
		r.Post("/api/v1/staff/products/{productID}/images", handler.confirm)
		r.Delete("/api/v1/staff/products/{productID}/images/{imageID}", handler.delete)
	})
}

// Wire shapes match openapi.yaml (ImageUploadUrlRequest / ImageUploadUrl / ImageConfirmRequest /
// ProductImage). ContentType on confirm is the type the client declared; the service cross-checks it
// against the file's magic bytes.
type uploadURLRequest struct {
	ContentType string `json:"contentType"`
}

type uploadURLResponse struct {
	UploadURL string    `json:"uploadUrl"`
	ObjectKey string    `json:"objectKey"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type confirmImageRequest struct {
	ObjectKey   string `json:"objectKey"`
	ContentType string `json:"contentType"`
}

type productImageResponse struct {
	ImageID   int64  `json:"imageId"`
	URL       string `json:"url"`
	SortOrder int    `json:"sortOrder"`
	IsPrimary bool   `json:"isPrimary"`
}

func (h *ImageHandler) presignUpload(w http.ResponseWriter, r *http.Request) {
	productID, ok := pathID(w, r, "productID")
	if !ok {
		return
	}
	var request uploadURLRequest
	if !decode(w, r, &request) {
		return
	}
	result, err := h.service.PresignUpload(r.Context(), productID, request.ContentType)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, uploadURLResponse{UploadURL: result.URL, ObjectKey: result.ObjectKey, ExpiresAt: time.Now().UTC().Add(app.PresignTTL)})
}

func (h *ImageHandler) confirm(w http.ResponseWriter, r *http.Request) {
	productID, ok := pathID(w, r, "productID")
	if !ok {
		return
	}
	var request confirmImageRequest
	if !decode(w, r, &request) {
		return
	}
	image, err := h.service.Confirm(r.Context(), productID, request.ObjectKey, request.ContentType)
	if err != nil {
		writeImageError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, productImageResponse{ImageID: image.ID, URL: h.baseURL + "/" + image.ObjectKey})
}

func (h *ImageHandler) delete(w http.ResponseWriter, r *http.Request) {
	productID, ok := pathID(w, r, "productID")
	if !ok {
		return
	}
	imageID, ok := pathID(w, r, "imageID")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), productID, imageID); err != nil {
		writeImageError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeImageError(w http.ResponseWriter, err error) {
	writeServiceError(w, err)
}
