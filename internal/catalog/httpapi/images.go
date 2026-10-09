package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/httpx"
)

type ImageHandler struct {
	service *app.ImageService
}

func NewImageHandler(service *app.ImageService) *ImageHandler {
	return &ImageHandler{service: service}
}

func RegisterImageRoutes(r chi.Router, handler *ImageHandler) {
	r.Route("/api/v1/staff", func(r chi.Router) {
		r.Use(auth.RequirePermission("catalog:image:write"))
		r.Post("/products/{productID}/images/upload-url", handler.presignUpload)
		r.Post("/products/{productID}/images", handler.confirm)
		r.Delete("/products/{productID}/images/{imageID}", handler.delete)
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
