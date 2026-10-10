package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/shared/httpx"
)

type ImageHandler struct {
	service *app.ImageService
}

func NewImageHandler(service *app.ImageService) *ImageHandler {
	return &ImageHandler{service: service}
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

type uploadURLRequest struct {
	ContentType string `json:"content_type"`
}

type confirmImageRequest struct {
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
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
	httpx.WriteJSON(w, http.StatusOK, result)
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
	httpx.WriteJSON(w, http.StatusCreated, image)
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
